// Command mqload publishes synthetic Task completion events and measures how
// fast the Payment consumer drains the queue. It is a load harness, not a
// business E2E test: events deliberately have no payment order, so the handler
// exercises inbox writes, task/settlement lookups and ACKs without moving
// money. Use it to compare Payment instance counts on the same machine.
//
//	go run ./tools/mqload -count 1000 -queue payment.task-events
package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"cityflow/internal/domain/task"
	"cityflow/internal/infrastructure/mq"
	"cityflow/internal/platform/messaging"
)

type queueInfo struct {
	MessagesReady          int64 `json:"messages_ready"`
	MessagesUnacknowledged int64 `json:"messages_unacknowledged"`
	Consumers              int64 `json:"consumers"`
	MessageStats           struct {
		PublishDetails struct {
			Rate float64 `json:"rate"`
		} `json:"publish_details"`
		DeliverGetDetails struct {
			Rate float64 `json:"rate"`
		} `json:"deliver_get_details"`
	} `json:"message_stats"`
}

type result struct {
	Count            int     `json:"count"`
	PublishSeconds   float64 `json:"publish_seconds"`
	PublishPerSecond float64 `json:"publish_per_second"`
	DrainSeconds     float64 `json:"drain_seconds"`
	WallSeconds      float64 `json:"wall_seconds"`
	ConsumePerSecond float64 `json:"consume_per_second"`
	PerConsumer      float64 `json:"per_consumer_per_second"`
	Consumers        int64   `json:"consumers"`
	BaselineReady    int64   `json:"baseline_ready"`
	BaselineUnacked  int64   `json:"baseline_unacked"`
	RemainingReady   int64   `json:"remaining_ready"`
	RemainingUnacked int64   `json:"remaining_unacked"`
	DBProcessed      int64   `json:"db_processed"`
	DBFailed         int64   `json:"db_failed"`
	SeededOrders     int     `json:"seeded_orders"`
}

func main() {
	var (
		count      = flag.Int("count", 1000, "number of events to publish")
		publishers = flag.Int("publishers", 4, "parallel confirmed publishers")
		queue      = flag.String("queue", "payment.task-events", "queue to drain")
		rabbitURL  = flag.String("rabbitmq-url", "", "AMQP URL (default: env/.env RABBITMQ_URL)")
		management = flag.String("management-url", "", "Management API URL (default: env/.env RABBITMQ_MANAGEMENT_URL)")
		dsn        = flag.String("payment-dsn", "", "Payment MySQL DSN (default: env/.env PAYMENT_DATABASE_DSN)")
		timeout    = flag.Duration("timeout", 10*time.Minute, "max drain wait")
		envFile    = flag.String("env-file", ".env", "dotenv file used when flags/env are empty")
		jsonOutput = flag.Bool("json", false, "print only the JSON result")
		seedOrders = flag.Bool("seed-orders", false, "insert matching PAID payment_orders so the settlement write is exercised")
		cleanup    = flag.Bool("cleanup", false, "delete load-created orders, settlements and inbox rows after the run")
	)
	flag.Parse()
	if *count <= 0 {
		log.Fatal("-count must be positive")
	}
	if *publishers <= 0 || *publishers > 32 {
		log.Fatal("-publishers must be between 1 and 32")
	}

	dotenv := readDotEnv(*envFile)
	if *rabbitURL == "" {
		*rabbitURL = firstNonEmpty(os.Getenv("RABBITMQ_URL"), dotenv["RABBITMQ_URL"])
	}
	if *management == "" {
		*management = firstNonEmpty(os.Getenv("RABBITMQ_MANAGEMENT_URL"), dotenv["RABBITMQ_MANAGEMENT_URL"], "http://127.0.0.1:15673")
	}
	if strings.TrimSpace(*rabbitURL) == "" {
		log.Fatal("RABBITMQ_URL is required (-rabbitmq-url or .env)")
	}
	if strings.TrimSpace(*management) == "" {
		log.Fatal("RABBITMQ_MANAGEMENT_URL is required (-management-url or .env)")
	}
	if *dsn == "" {
		*dsn = firstNonEmpty(os.Getenv("PAYMENT_DATABASE_DSN"), dotenv["PAYMENT_DATABASE_DSN"])
	}
	var db *sql.DB
	if strings.TrimSpace(*dsn) != "" {
		opened, openErr := openPaymentDB(*dsn)
		if openErr != nil {
			log.Fatalf("open Payment database for inbox verification: %v", openErr)
		}
		db = opened
		defer db.Close()
	} else {
		fmt.Fprintln(os.Stderr, "warning: PAYMENT_DATABASE_DSN is empty; falling back to RabbitMQ depth only (drain time may be stale)")
	}
	runID := fmt.Sprintf("%d", time.Now().UnixNano())
	seeded := 0
	if *seedOrders {
		if db == nil {
			log.Fatal("-seed-orders requires PAYMENT_DATABASE_DSN")
		}
		seededCount, seedErr := seedPaidOrders(context.Background(), db, runID, *count)
		if seedErr != nil {
			log.Fatalf("seed paid orders: %v", seedErr)
		}
		seeded = seededCount
	}

	vhost, auth := parseAMQP(*rabbitURL)
	client := &http.Client{Timeout: 5 * time.Second}
	baseline, err := fetchQueue(client, *management, vhost, *queue, auth)
	if err != nil {
		log.Fatalf("read baseline queue: %v", err)
	}
	if baseline.Consumers == 0 {
		log.Fatalf("queue %s has 0 consumers; start the Payment service first", *queue)
	}
	if baseline.MessagesReady > 0 || baseline.MessagesUnacknowledged > 0 {
		fmt.Fprintf(os.Stderr, "warning: queue is not empty (ready=%d unacked=%d); drain time includes existing backlog\n",
			baseline.MessagesReady, baseline.MessagesUnacknowledged)
	}

	start := time.Now()
	var published atomic.Int64
	var errOnce sync.Once
	var firstErr error
	var wg sync.WaitGroup
	perPublisher := (*count + *publishers - 1) / *publishers
	for worker := 0; worker < *publishers; worker++ {
		begin := worker * perPublisher
		end := begin + perPublisher
		if end > *count {
			end = *count
		}
		if begin >= end {
			continue
		}
		wg.Add(1)
		go func(begin, end int) {
			defer wg.Done()
			pub := mq.NewPublisher(*rabbitURL)
			defer pub.Close()
			for i := begin; i < end; i++ {
				msg, err := syntheticEvent(runID, i)
				if err != nil {
					errOnce.Do(func() { firstErr = err })
					return
				}
				if err := pub.Publish(context.Background(), msg); err != nil {
					errOnce.Do(func() { firstErr = err })
					return
				}
				published.Add(1)
			}
		}(begin, end)
	}
	wg.Wait()
	if firstErr != nil {
		log.Fatalf("publish failed after %d/%d: %v", published.Load(), *count, firstErr)
	}
	publishDuration := time.Since(start)
	publishRate := float64(*count) / publishDuration.Seconds()

	drainStart := time.Now()
	deadline := time.Now().Add(*timeout)
	var info queueInfo
	var dbProcessed, dbFailed int64
	if db != nil {
		dbProcessed, dbFailed, err = waitInbox(context.Background(), db, runID, *count, *timeout)
		if err != nil {
			fmt.Fprintf(os.Stderr, "inbox wait: %v\n", err)
		}
		// Management API queue depth can trail the broker by a few seconds; wait
		// for an empty queue so the final report is not internally contradictory
		// (all inbox rows processed while "ready" still shows a backlog).
		info, err = waitQueueEmpty(client, *management, vhost, *queue, auth, 15*time.Second)
		if err != nil {
			fmt.Fprintf(os.Stderr, "queue settle: %v\n", err)
		}
	} else {
		for {
			info, err = fetchQueue(client, *management, vhost, *queue, auth)
			if err != nil {
				log.Fatalf("read queue during drain: %v", err)
			}
			if info.MessagesReady == 0 && info.MessagesUnacknowledged == 0 {
				break
			}
			if time.Now().After(deadline) {
				fmt.Fprintf(os.Stderr, "timeout: ready=%d unacked=%d consumers=%d\n",
					info.MessagesReady, info.MessagesUnacknowledged, info.Consumers)
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
	}
	drainDuration := time.Since(drainStart)
	wallDuration := time.Since(start)
	consumeRate := 0.0
	perConsumer := 0.0
	if wallDuration > 0 {
		consumeRate = float64(*count) / wallDuration.Seconds()
		if info.Consumers > 0 {
			perConsumer = consumeRate / float64(info.Consumers)
		}
	}

	out := result{
		Count: *count, PublishSeconds: round3(publishDuration.Seconds()), PublishPerSecond: round1(publishRate),
		DrainSeconds: round3(drainDuration.Seconds()), WallSeconds: round3(wallDuration.Seconds()),
		ConsumePerSecond: round1(consumeRate),
		PerConsumer:      round1(perConsumer), Consumers: info.Consumers,
		BaselineReady: baseline.MessagesReady, BaselineUnacked: baseline.MessagesUnacknowledged,
		RemainingReady: info.MessagesReady, RemainingUnacked: info.MessagesUnacknowledged,
		DBProcessed: dbProcessed, DBFailed: dbFailed, SeededOrders: seeded,
	}
	if *jsonOutput {
		encoded, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(encoded))
	} else {
		fmt.Printf("published=%d in %.3fs (%.1f/s)\n", out.Count, out.PublishSeconds, out.PublishPerSecond)
		fmt.Printf("drained in %.3fs; wall %.3fs (%.1f msg/s, %.1f per consumer, consumers=%d)\n",
			out.DrainSeconds, out.WallSeconds, out.ConsumePerSecond, out.PerConsumer, out.Consumers)
		fmt.Printf("baseline ready=%d unacked=%d; remaining ready=%d unacked=%d\n",
			out.BaselineReady, out.BaselineUnacked, out.RemainingReady, out.RemainingUnacked)
		if db != nil {
			fmt.Printf("inbox processed=%d failed=%d\n", out.DBProcessed, out.DBFailed)
			if out.SeededOrders > 0 {
				fmt.Printf("seeded paid orders=%d (real settlement write path)\n", out.SeededOrders)
			}
		}
	}
	if *cleanup && db != nil {
		if err := cleanupLoad(context.Background(), db, runID); err != nil {
			fmt.Fprintf(os.Stderr, "cleanup load rows: %v\n", err)
		}
	}
	if db != nil && dbProcessed < int64(*count) {
		os.Exit(1)
	}
	if db == nil && (info.MessagesReady > 0 || info.MessagesUnacknowledged > 0) {
		os.Exit(1)
	}
}

func waitQueueEmpty(client *http.Client, baseURL, vhost, queue, auth string, timeout time.Duration) (queueInfo, error) {
	deadline := time.Now().Add(timeout)
	var info queueInfo
	var err error
	for {
		info, err = fetchQueue(client, baseURL, vhost, queue, auth)
		if err != nil {
			return info, err
		}
		if info.MessagesReady == 0 && info.MessagesUnacknowledged == 0 {
			return info, nil
		}
		if time.Now().After(deadline) {
			return info, fmt.Errorf("queue still has ready=%d unacked=%d", info.MessagesReady, info.MessagesUnacknowledged)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func seedPaidOrders(ctx context.Context, db *sql.DB, runID string, count int) (int, error) {
	const payerID = "00000000-0000-4000-8000-000000000001"
	now := time.Now().UTC()
	expires := now.Add(time.Hour)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	seeded := 0
	for i := 0; i < count; i++ {
		orderID := newUUID()
		taskID := loadTaskID(runID, i)
		providerOrderNo := "LOAD-" + orderID
		idempotency := fmt.Sprintf("load-%s-%d", runID, i)
		if _, err := tx.ExecContext(ctx, `INSERT INTO payment_orders
			(id, task_id, payer_id, original_amount_fen, discount_fen, amount_fen, currency, status,
			 provider, provider_order_no, idempotency_key, paid_at, expires_at, created_at, updated_at)
			VALUES (?, ?, ?, 100, 0, 100, 'CNY', 'PAID', 'MOCK', ?, ?, ?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE status = 'PAID', amount_fen = 100, updated_at = VALUES(updated_at)`,
			orderID, taskID, payerID, providerOrderNo, idempotency, now, expires, now, now,
		); err != nil {
			return seeded, err
		}
		seeded++
	}
	if err := tx.Commit(); err != nil {
		return seeded, err
	}
	return seeded, nil
}

func cleanupLoad(ctx context.Context, db *sql.DB, runID string) error {
	pattern := "load-" + runID + "-%"
	if _, err := db.ExecContext(ctx, "DELETE FROM rider_settlements WHERE task_id LIKE ?", pattern); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, "DELETE FROM payment_orders WHERE task_id LIKE ?", pattern); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, "DELETE FROM payment_inbox WHERE event_id LIKE ?", pattern); err != nil {
		return err
	}
	return nil
}

func loadTaskID(runID string, index int) string {
	return fmt.Sprintf("load-%s-%d", runID, index)
}

func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("00000000-0000-4000-8000-%012d", time.Now().UnixNano()%1000000000000)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]), hex.EncodeToString(b[4:6]), hex.EncodeToString(b[6:8]),
		hex.EncodeToString(b[8:10]), hex.EncodeToString(b[10:16]))
}

func openPaymentDB(dsn string) (*sql.DB, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func waitInbox(ctx context.Context, db *sql.DB, runID string, count int, timeout time.Duration) (int64, int64, error) {
	deadline := time.Now().Add(timeout)
	pattern := "load-" + runID + "-%"
	var processed, failed int64
	for {
		err := db.QueryRowContext(ctx,
			`SELECT COALESCE(SUM(status = 'PROCESSED'), 0), COALESCE(SUM(status = 'FAILED'), 0)
			 FROM payment_inbox WHERE event_id LIKE ?`, pattern,
		).Scan(&processed, &failed)
		if err != nil {
			return processed, failed, err
		}
		if processed >= int64(count) {
			return processed, failed, nil
		}
		if time.Now().After(deadline) {
			return processed, failed, fmt.Errorf("timed out with processed=%d failed=%d of %d", processed, failed, count)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func syntheticEvent(runID string, index int) (messaging.Message, error) {
	id := fmt.Sprintf("load-%s-%d", runID, index)
	amount := int64(100)
	payload, err := json.Marshal(task.Task{
		ID: id, Title: "mq load event", Type: "DELIVERY", Status: task.StatusCompleted,
		CreatedBy: "load-user", WorkerID: "load-rider", QuotedAmountFen: &amount,
	})
	if err != nil {
		return messaging.Message{}, err
	}
	return messaging.Message{
		ID: id, Type: "task.completed.v1", AggregateID: id,
		Payload: payload, OccurredAt: time.Now().UTC(),
	}, nil
}

func fetchQueue(client *http.Client, baseURL, vhost, queue, auth string) (queueInfo, error) {
	var info queueInfo
	endpoint := fmt.Sprintf("%s/api/queues/%s/%s", strings.TrimRight(baseURL, "/"),
		url.PathEscape(vhost), url.PathEscape(queue))
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return info, err
	}
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	resp, err := client.Do(req)
	if err != nil {
		return info, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return info, fmt.Errorf("management API %s: %s", endpoint, resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return info, err
	}
	return info, nil
}

func parseAMQP(raw string) (vhost, auth string) {
	vhost = "/"
	parsed, err := url.Parse(raw)
	if err != nil {
		return vhost, ""
	}
	path := strings.Trim(parsed.Path, "/")
	if path != "" {
		if decoded, err := url.PathUnescape(path); err == nil {
			vhost = decoded
		} else {
			vhost = path
		}
	}
	if parsed.User != nil {
		user := parsed.User.Username()
		pass, _ := parsed.User.Password()
		auth = "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
	}
	return vhost, auth
}

func readDotEnv(path string) map[string]string {
	values := map[string]string{}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return values
	}
	data, err := os.ReadFile(absolute)
	if err != nil {
		return values
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') ||
			(value[0] == '\'' && value[len(value)-1] == '\'')) {
			value = value[1 : len(value)-1]
		}
		values[key] = value
	}
	return values
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func round1(value float64) float64 { return float64(int64(value*10+0.5)) / 10 }
func round3(value float64) float64 { return float64(int64(value*1000+0.5)) / 1000 }
