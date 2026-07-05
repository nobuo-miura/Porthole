# Porthole

[English](README.md) | [日本語](README.ja.md)

Dockerで動作する軽量なWebベースの接続テストツールです。
データベース、キャッシュ、メッセージキュー、任意のTCP/UDPポートへの到達性と認証を、ブラウザからすばやく確認できます。

![Go](https://img.shields.io/badge/Go-1.26.1-blue) ![Docker](https://img.shields.io/badge/Docker-ready-blue) ![License](https://img.shields.io/badge/license-MIT-green)

![Porthole screenshot](docs/screenshot.png)

## 機能

- **TCP / UDP** — レイテンシ付きの生ポート接続チェック
- **MySQL / MariaDB** — ping、バージョン、認証済みユーザーの確認
- **PostgreSQL** — ping、バージョン、認証済みユーザーの確認
- **SQL Server** — ping、バージョン、認証済みユーザーの確認
- **MongoDB** — `connectionStatus` によるpingと認証済みユーザーの確認
- **Redis** — `PING` コマンドとパスワード認証
- **Elasticsearch** — `/_cluster/health` エンドポイント
- **RabbitMQ** — AMQPハンドシェイク
- **SMTP** — `EHLO` ハンドシェイク（メールは送信しません）
- **SSL/TLS** — プロトコルごとに設定可能（`disable` / `require` / `skip-verify` / `verify-ca` / `verify-full`）
- **バッチモード** — `host:port` の一覧を貼り付けて並行テスト
- **履歴** — 直近50件のチェック結果をメモリ上に保存

## クイックスタート

### Docker Hubから起動（推奨）

```bash
docker run -p 8080:8080 nobuomiura/porthole:latest
```

### Docker Composeでビルドして起動

```bash
docker compose up --build
```

ブラウザで **http://localhost:8080** を開きます。

### ポートを変更する

```bash
PORT=9090 docker compose up --build
```

### Dockerホスト上のサービスをテストする

`docker-compose.yml` の `extra_hosts` ブロックのコメントを外します。

```yaml
extra_hosts:
  - "host.docker.internal:host-gateway"
```

その後、UIのホスト名として `host.docker.internal` を使用します。

## ローカル実行（Dockerなし）

```bash
go run .
# or
make run
```

Go 1.26.1以上が必要です。

## API

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/check` | 単一の接続チェックを実行 |
| `POST` | `/api/check/batch` | 複数のTCPチェックを並行実行 |
| `GET`  | `/api/history` | 直近N件のチェック結果を取得 |
| `GET`  | `/healthz` | ヘルスプローブ |

### 例

```bash
curl -X POST http://localhost:8080/api/check \
  -H 'Content-Type: application/json' \
  -d '{
    "type": "postgres",
    "host": "db.example.com",
    "port": 5432,
    "username": "postgres",
    "password": "secret",
    "database": "myapp",
    "ssl_mode": "require",
    "timeout_sec": 5
  }'
```

```json
{
  "success": true,
  "type": "postgres",
  "host": "db.example.com",
  "port": 5432,
  "latency_ms": 12,
  "detail": "PostgreSQL 16.2 on x86_64 | authenticated as postgres",
  "checked_at": "2026-03-21T10:00:00Z"
}
```

### 対応している `type` の値

`tcp`, `udp`, `mysql`, `mariadb`, `postgres`, `postgresql`, `mongodb`, `redis`, `elasticsearch`, `rabbitmq`, `smtp`, `sqlserver`, `mssql`

### プロトコルごとのSSLモード

| Protocol | Supported values |
|---|---|
| MySQL / MariaDB | `disable`, `skip-verify`, `require` |
| PostgreSQL | `disable`, `require`, `verify-ca`, `verify-full` |
| MongoDB | `disable`, `skip-verify`, `require` |
| Redis | `disable`, `skip-verify`, `require` |
| SQL Server | `disable`, `skip-verify`, `require` |

## ライセンス

MIT
