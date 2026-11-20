package cache

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

var ErrMapQuota = errors.New("地图请求频率达到上限，请稍后再试")

// MapCache has its own bounded Redis namespace; it never changes Redis global
// maxmemory/policies and cannot evict rider GEO, auth or durable business data.
type MapCache struct {
	redis      *Redis
	prefix     string
	budget     int64
	flights    singleflight.Group
	mu         sync.Mutex
	active     int
	quotaUntil time.Time
	quotaCount int
	rate       int
	ctx        context.Context
	cancel     context.CancelFunc
}

func NewMapCache(r *Redis, budget int64, rate int) *MapCache {
	ctx, cancel := context.WithCancel(context.Background())
	if budget < 65536 {
		budget = 128 << 20
	}
	if rate < 1 {
		rate = 60
	}
	return &MapCache{redis: r, prefix: "cityflow:{map-cache}:v2:", budget: budget, rate: rate, ctx: ctx, cancel: cancel}
}
func (m *MapCache) Close()        { m.cancel() }
func mapDigest(key string) string { h := sha256.Sum256([]byte(key)); return hex.EncodeToString(h[:]) }
func (m *MapCache) keys() []string {
	return []string{m.prefix + "data", m.prefix + "lru", m.prefix + "expiry", m.prefix + "hits", m.prefix + "hit-expiry"}
}

// Admission retains at most 21 timestamps per key for an exact rolling ten
// minute threshold; at most 4096 tracked queries prevents cardinality attacks.
const mapLookupLua = `
local now=tonumber(ARGV[2]); local k=ARGV[1]
local function drop(key) redis.call('DEL',ARGV[4]..key);redis.call('HDEL',KEYS[1],key);redis.call('ZREM',KEYS[2],key);redis.call('ZREM',KEYS[3],key) end
for _,v in ipairs(redis.call('ZRANGEBYSCORE',KEYS[3],'-inf',now,'LIMIT',0,128)) do drop(v) end
for _,v in ipairs(redis.call('ZRANGEBYSCORE',KEYS[5],'-inf',now,'LIMIT',0,128)) do redis.call('HDEL',KEYS[4],v);redis.call('ZREM',KEYS[5],v) end
local old=redis.call('HGET',KEYS[4],k);local hits={}; if old then for _,t in ipairs(cjson.decode(old)) do if t>now-600000 then table.insert(hits,t) end end end
table.insert(hits,now);if #hits>21 then table.remove(hits,1) end
redis.call('HSET',KEYS[4],k,cjson.encode(hits));redis.call('ZADD',KEYS[5],now+600000,k)
while redis.call('ZCARD',KEYS[5])>4096 do local v=redis.call('ZRANGE',KEYS[5],0,0)[1];redis.call('HDEL',KEYS[4],v);redis.call('ZREM',KEYS[5],v) end
local budget=tonumber(ARGV[3]);local function memory() local n=0;for _,key in ipairs(KEYS) do n=n+(redis.call('MEMORY','USAGE',key) or 0) end;for _,v in ipairs(redis.call('HVALS',KEYS[1])) do n=n+tonumber(v) end;return n end
while memory()>budget do local v=redis.call('ZRANGE',KEYS[2],0,0)[1];if v then drop(v) else v=redis.call('ZRANGE',KEYS[5],0,0)[1];if not v then break end;redis.call('HDEL',KEYS[4],v);redis.call('ZREM',KEYS[5],v) end end
for _,key in ipairs(KEYS) do redis.call('PEXPIRE',key,691200000) end
local expires=redis.call('ZSCORE',KEYS[3],k);if expires and tonumber(expires)<=now then drop(k) end
local value=redis.call('GET',ARGV[4]..k);if value then redis.call('ZADD',KEYS[2],now,k) end
return {value or '',#hits}`
const mapStoreLua = `
if redis.call('GET',KEYS[6])~=ARGV[5] then return 0 end
local k=ARGV[1];local payload=ARGV[7]..k;local ttl=tonumber(ARGV[4])-tonumber(ARGV[3]);if ttl<=0 then return 0 end;redis.call('SET',payload,ARGV[2],'PX',ttl);redis.call('HSET',KEYS[1],k,redis.call('MEMORY','USAGE',payload) or string.len(ARGV[2]));redis.call('ZADD',KEYS[2],ARGV[3],k);redis.call('ZADD',KEYS[3],ARGV[4],k)
local function memory() local n=0;for i=1,5 do n=n+(redis.call('MEMORY','USAGE',KEYS[i]) or 0) end;for _,v in ipairs(redis.call('HVALS',KEYS[1])) do n=n+tonumber(v) end;return n end
while redis.call('ZCARD',KEYS[2])>4096 or memory()>tonumber(ARGV[6]) do local v=redis.call('ZRANGE',KEYS[2],0,0)[1];if not v then break end;redis.call('DEL',ARGV[7]..v);redis.call('HDEL',KEYS[1],v);redis.call('ZREM',KEYS[2],v);redis.call('ZREM',KEYS[3],v) end
for i=1,5 do redis.call('PEXPIRE',KEYS[i],691200000) end
return 1`

type mapEnvelope struct {
	Body  json.RawMessage `json:"body"`
	Fresh int64           `json:"fresh"`
}
type MapPolicy struct {
	TTL, Stale time.Duration
	Force      bool
	Generation string
}

func (m *MapCache) Generation(ctx context.Context) (string, error) {
	if m.redis == nil {
		return "", errors.New("cache unavailable")
	}
	key := m.prefix + "generation"
	var token [16]byte
	_, _ = rand.Read(token[:])
	initial := hex.EncodeToString(token[:])
	if err := m.redis.client.SetNX(ctx, key, initial, 0).Err(); err != nil {
		return "", err
	}
	return m.redis.client.Get(ctx, key).Result()
}
func (m *MapCache) Invalidate(ctx context.Context, keys ...string) error {
	if m.redis == nil {
		return nil
	}
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return err
	}
	script := `redis.call('SET',KEYS[6],ARGV[1]);for i=3,#ARGV do redis.call('DEL',ARGV[2]..ARGV[i]);redis.call('HDEL',KEYS[1],ARGV[i]);redis.call('ZREM',KEYS[2],ARGV[i]);redis.call('ZREM',KEYS[3],ARGV[i]) end;return 1`
	args := []any{hex.EncodeToString(token[:]), m.prefix + "entry:"}
	for _, key := range keys {
		args = append(args, mapDigest(key))
	}
	return m.redis.client.Eval(ctx, script, append(m.keys(), m.prefix+"generation"), args...).Err()
}
func (m *MapCache) Allow(ctx context.Context) error {
	// A Redis counter bounds provider calls across processes. When Redis is down
	// retain a conservative per-process rate rather than disabling all protection.
	if m.redis != nil {
		key := m.prefix + "quota:" + fmt.Sprint(time.Now().Unix()/60)
		n, err := m.redis.client.Eval(ctx, `local n=redis.call('INCR',KEYS[1]);if n==1 then redis.call('EXPIRE',KEYS[1],120) end;return n`, []string{key}).Int64()
		if err == nil {
			if n > int64(m.rate) {
				return ErrMapQuota
			}
			return nil
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	if !now.Before(m.quotaUntil) {
		m.quotaUntil = now.Add(time.Minute)
		m.quotaCount = 0
	}
	if m.quotaCount >= m.rate {
		return ErrMapQuota
	}
	m.quotaCount++
	return nil
}
func (m *MapCache) Load(ctx context.Context, key string, p MapPolicy, load func(context.Context) ([]byte, error)) ([]byte, error) {
	digest := mapDigest(key)
	admit := p.Force
	var cached mapEnvelope
	gen, gerr := m.Generation(ctx)
	if p.Generation != "" && gen != p.Generation {
		p.TTL = 0
	}
	if m.redis != nil && gerr == nil {
		values, err := m.redis.client.Eval(ctx, mapLookupLua, m.keys(), digest, time.Now().UnixMilli(), m.budget, m.prefix+"entry:").Slice()
		if err == nil && len(values) == 2 {
			if count, ok := values[1].(int64); ok && count > 20 {
				admit = true
			}
			if raw, ok := values[0].(string); ok && raw != "" {
				_ = json.Unmarshal([]byte(raw), &cached)
			}
		}
	}
	run := func() (any, error) {
		// Bound unique concurrent misses as well as coalescing identical requests.
		m.mu.Lock()
		if m.active >= 32 {
			m.mu.Unlock()
			return nil, ErrMapQuota
		}
		m.active++
		m.mu.Unlock()
		defer func() { m.mu.Lock(); m.active--; m.mu.Unlock() }()
		requestCtx, cancel := context.WithTimeout(m.ctx, 12*time.Second)
		defer cancel()
		body, err := load(requestCtx)
		if err != nil {
			return nil, err
		}
		if admit && p.TTL > 0 && len(body) <= 1<<20 && gerr == nil {
			now := time.Now()
			encoded, _ := json.Marshal(mapEnvelope{Body: body, Fresh: now.Add(p.TTL).UnixMilli()})
			_ = m.redis.client.Eval(requestCtx, mapStoreLua, append(m.keys(), m.prefix+"generation"), digest, string(encoded), now.UnixMilli(), now.Add(p.TTL+p.Stale).UnixMilli(), gen, m.budget, m.prefix+"entry:").Err()
		}
		return body, nil
	}
	flightKey := digest + ":" + gen
	if len(cached.Body) > 0 && p.TTL > 0 {
		if cached.Fresh > time.Now().UnixMilli() {
			return cached.Body, nil
		}
		// SWR only where allowed by policy (city boundaries); merchant cells use no stale period.
		if p.Stale > 0 {
			m.flights.DoChan(flightKey, run)
			return cached.Body, nil
		}
	}
	ch := m.flights.DoChan(flightKey, run)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-ch:
		if result.Err != nil {
			return nil, result.Err
		}
		return result.Val.([]byte), nil
	}
}
