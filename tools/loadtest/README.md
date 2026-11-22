# CityFlow HTTP 压测工具

放这里是本地工具，二进制在 `bin/`，已被项目 `.gitignore` 的 `*.exe` 规则忽略。下面的命令默认在 `tools/loadtest` 目录执行。

## 已安装

| 工具 | 版本 | 用途 | 启动方式 |
|---|---|---|---|
| `k6` | v2.4.0 | 脚本化场景、固定 QPS、阈值和 p95/p99 | `.\bin\k6.exe` |
| `hey` | v0.1.5 | 临时发压、快速看 requests/sec | `.\bin\hey.exe` |
| `vegeta` | v12.13.0 | 恒定 RPS、延迟分布和结果归档 | `.\bin\vegeta.exe` |

`wrk` 不原生支持 Windows，也没有在这里安装；需要它时用 WSL。

## 先确认服务

```powershell
curl.exe http://127.0.0.1:8087/readyz
```

压测机和被压服务尽量分开。本机上同时跑 Gateway、五个 gRPC 服务和压测进程，QPS 结果会被 CPU、端口和客户端本身限制，不能直接当作服务上限。

## 1. k6：固定到达速率

`hot.js` 默认打公开热店榜接口。`RATE` 是目标 QPS，`DURATION` 是持续时间：

```powershell
.\bin\k6.exe run .\hot.js `
  -e BASE_URL=http://127.0.0.1:8087 `
  -e CATEGORY=FOOD `
  -e RATE=50 `
  -e DURATION=30s
```

历史月加 `MONTH=202610`。结果里的 `http_reqs` 是实际完成请求速率，`http_req_duration` 看 p95/p99，`http_req_failed` 看错误率。

如果只想快速跑一次：

```powershell
.\bin\k6.exe run .\hot.js -e RATE=100 -e DURATION=15s
```

## 2. hey：临时快速发压

```powershell
.\bin\hey.exe -n 10000 -c 50 "http://127.0.0.1:8087/api/v1/merchants/hot?category=FOOD&limit=20"
```

`-n` 是总请求数，`-c` 是并发数。输出重点看 `Requests/sec`、`Average`、`P95`、`P99` 和非 2xx 数量。

## 3. vegeta：恒定 RPS

先建目标文件：

```powershell
@'
GET http://127.0.0.1:8087/api/v1/merchants/hot?category=FOOD&limit=20
'@ | Set-Content -Encoding utf8 .\targets-hot.txt
```

100 RPS 持续 30 秒：

```powershell
.\bin\vegeta.exe attack -duration=30s -rate=100 -targets .\targets-hot.txt -output .\hot.bin
.\bin\vegeta.exe report .\hot.bin
```

`vegeta` 的原始结果是二进制，不要用 PowerShell 管道直接接 `report`，否则可能被编码成文本导致报告失败。

需要保存原始结果时，上面的 `-output` 已经是保存方式；也可以把 `hot.bin` 留给后续 `report`、`plot` 使用。

## 口径

- 先做 1 分钟空载基线，再逐级加 QPS；每一级至少跑 30 秒，记录 `实际 QPS`、`p95`、`p99`、`错误率`、`Gateway/服务 CPU`。
- 出现 `http_req_failed` 上升或 p99 明显抬头时，那一级就是拐点，不要只报一个最高 QPS。
- 公开热榜接口不带登录态；如果要压点赞或商家接口，需要单独处理 JWT，且写接口会改数据，不要拿生产账号直接打。
