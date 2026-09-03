# vacaro-back

## アプリの目的

日程調整ツール

## このアプリの特徴

枠を設定してそこに行ける行けないではなく，自分がいける時間のin,outだけ入力するツール

## 認証

ユーザはGoogle認証を行うことでGoogleカレンダーとの連携機能を使用できるが，認証を行わなくともゲストユーザとして仮登録できる

## Dockerを使った開発

### 必要なもの

- Docker EngineまたはDocker Desktop
- Docker Compose v2

GoとPostgreSQLをホストへ個別にインストールする必要はありません。

### 初回起動

環境変数のサンプルをコピーします。

```bash
cp .env.example .env
```

APIとPostgreSQLを起動します。

```bash
docker compose up --build
```

起動時に未SQLマイグレーションが自動適用され、完了後にAPIが起動します。

別のターミナルからAPIを確認します。

```bash
curl http://localhost:8080/health
curl http://localhost:8080/ready
```

両方から次のレスポンスが返れば、APIとPostgreSQLの起動成功です。

```json
{"status":"ok"}
```

### よく使うコマンド

```bash
# バックグラウンドで起動
docker compose up --build -d

# ログを確認
docker compose logs -f api

# コンテナとヘルスチェックの状態を確認
docker compose ps

# ソースコード変更後にAPIを再起動
docker compose restart api

# テストを実行
docker compose exec api go test ./...

# Goモジュールを整理
docker compose exec api go mod tidy

# PostgreSQLへ接続
docker compose exec db psql -U vacaro -d vacaro

# 適用済みマイグレーションを確認
docker compose exec db psql -U vacaro -d vacaro \
  -c "SELECT * FROM schema_migrations;"

# 停止
docker compose down
```

DBのデータも削除して初期化する場合だけ、次を実行します。

```bash
docker compose down -v
```

### 本番用イメージの確認

本番用ステージではGoコンパイラやソースコードを含まないイメージを生成します。

```bash
docker build --target production -t vacaro-api:local .
docker run --rm -p 8080:8080 \
  -e PORT=8080 \
  -e DATABASE_URL="${DATABASE_URL}" \
  vacaro-api:local
```

`DATABASE_URL`にはデプロイ先のPostgreSQL接続URLを設定します。APIは起動時にDB接続を確認し、接続できない場合はエラーで終了します。

## ヘルスチェック

- `GET /health`: APIプロセスが動作しているか確認するLivenessチェック
- `GET /ready`: PostgreSQLへ接続でき、リクエストを処理できるか確認するReadinessチェック

## 設計資料

- [データベース設計書](docs/database-design.md)
- [API設計書](docs/api-design.md)

## データベースマイグレーション

マイグレーションSQLは`db/migrations`に保存します。現在のMVPスキーマは次の順番で作成されます。

1. `users`
2. `events`
3. `participants`
4. `availability_intervals`

新しいマイグレーションを追加した後は、次のコマンドで適用できます。

```bash
docker compose run --rm migrate
```
