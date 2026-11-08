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
curl http://localhost:8081/health
curl http://localhost:8081/ready
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

本番環境では`.env.example`の`SESSION_KEY`を使用せず、暗号学的に安全な32バイト値をBase64エンコードしてSecret Managerなどから設定してください。

### ブラウザ保護とレート制限

状態を変更するAPIリクエストには`X-Vacaro-CSRF: same-origin`が必要です。ブラウザ向けフロントエンドはこのヘッダーを自動送信します。APIはさらに`Sec-Fetch-Site`と`Origin`を検証し、`ALLOWED_ORIGINS`（未指定時は`PUBLIC_APP_URL`のOrigin）以外からのunsafeリクエストを拒否します。CORSは有効化しておらず、フロントエンドとAPIを同一サイト配下で提供する前提です。

`/api/v1`にはクライアントIP単位のトークンバケット方式レート制限が適用されます。既定値は1分あたり120リクエストで、`API_RATE_LIMIT_REQUESTS`と`API_RATE_LIMIT_WINDOW`から変更できます。制限超過時は`429`と`Retry-After`を返します。

リバースプロキシ経由で実クライアントIPを利用する場合だけ、APIへ直接接続するプロキシのネットワークを`TRUSTED_PROXY_CIDRS`へCIDR形式で設定してください。未設定時は偽装可能な`X-Forwarded-For`と`X-Real-IP`を無視します。複数APIインスタンスで厳密な全体制限が必要な場合は、ロードバランサーや共有ストア側でもレート制限を設定してください。

## ヘルスチェック

- `GET /health`: APIプロセスが動作しているか確認するLivenessチェック
- `GET /ready`: PostgreSQLへ接続でき、リクエストを処理できるか確認するReadinessチェック

## 本番イメージの発行

`main`へのpush時にGitHub Actionsがテストと`go vet`を実行し、成功したコミットのAPIイメージとマイグレーションイメージをGHCRへ発行します。

```text
ghcr.io/astalum/vacaro-back:sha-<40桁のcommit SHA>
ghcr.io/astalum/vacaro-back:migrations-sha-<40桁のcommit SHA>
```

本番VPSではソースコードやGoコンパイラを配置せず、これらの不変タグをComposeからpullして使用します。マイグレーションイメージには`migrate`コマンドと`db/migrations`だけが含まれます。

## API

主なAPIは次のとおりです。詳しいリクエスト・レスポンスは[API仕様書](docs/api-design.md)を参照してください。

- `POST /api/v1/guest-sessions`: ゲストセッション作成
- `POST /api/v1/events`: イベント作成
- `GET /api/v1/invitations/{invite_token}`: 招待情報取得
- `POST /api/v1/invitations/{invite_token}/participants`: イベント参加
- `PUT /api/v1/events/{event_id}/my-availability`: 参加可能時間を回答
- `POST /api/v1/events/{event_id}/my-availability/copy`: 別イベントの自分の回答をコピー（Google認証必須）
- `GET /api/v1/events/{event_id}/candidates`: 候補日時を取得
- `PUT /api/v1/events/{event_id}/confirmation`: 日程確定
- `GET /api/v1/auth/google/start`: Googleログイン開始
- `POST /api/v1/integrations/google-calendar`: Googleカレンダー連携開始
- `DELETE /api/v1/integrations/google-calendar`: Googleカレンダー連携解除
- `POST /api/v1/events/{event_id}/google-calendar-availability/preview`: カレンダーから空き時間をプレビュー

### Google連携の設定

Google Cloud ConsoleでOAuth 2.0クライアントを作成し、次のリダイレクトURIを登録します。

```text
http://localhost:8080/api/v1/auth/google/callback
http://localhost:8080/api/v1/integrations/google-calendar/callback
```

`.env`の`GOOGLE_*`と`TOKEN_ENCRYPTION_KEY`をすべて設定するとGoogle連携が有効になります。`TOKEN_ENCRYPTION_KEY`には、`SESSION_KEY`とは別に生成したBase64形式の32バイト鍵を使用してください。未設定の場合もGoogle以外のAPIは起動します。

カレンダー連携では`calendar.events.freebusy`、`calendar.calendarlist.readonly`、`calendar.app.created`の権限を使用します。予定書き込み権限の追加前から接続済みのユーザーは、設定画面からGoogleカレンダーを再接続してください。

```bash
openssl rand -base64 32
```

### E2Eテスト

APIとDBを起動した状態で実行します。

```bash
# 高速なユニットテスト
docker compose exec -T api go test -count=1 ./internal/...

# 実PostgreSQLを使うRepository統合テスト
docker compose exec -T api sh -c \
  'TEST_DATABASE_URL="$DATABASE_URL" go test -count=1 ./internal/database ./internal/repository -v'

# 実HTTP APIとPostgreSQLを使うE2Eテスト
docker compose exec -T api sh -c \
  'E2E_BASE_URL=http://localhost:8080 E2E_DATABASE_URL="$DATABASE_URL" go test -count=1 ./tests/e2e -v'

# Race Detector
docker compose exec -T api go test -race ./internal/...
```

テストではゲスト2人によるイベント作成、招待参加、回答、候補計算、確定、取消を実行し、終了時にテストデータを削除します。

## 設計資料

- [データベース設計書](docs/database-design.md)
- [API仕様書](docs/api-design.md)

## データベースマイグレーション

マイグレーションSQLは`db/migrations`に保存します。現在のMVPスキーマは次の順番で作成されます。

1. `users`
2. `events`
3. `participants`
4. `availability_intervals`
5. `auth_accounts`、`google_calendar_connections`

新しいマイグレーションを追加した後は、次のコマンドで適用できます。

```bash
docker compose run --rm migrate
```

### 日時とユーザー設定のタイムゾーン

日時は内部・APIではUTCで管理し、DBセッションと日時デコードもUTCに統一します。ユーザーの入力・表示用タイムゾーンの初期値は`Asia/Tokyo`です。Google再ログイン時は保存済みの設定を保持します。マイグレーション`000014`は新規ユーザーのデフォルトのみ変更し、既存ユーザーの設定は変更しません。

Google の同意画面でカレンダー一覧・空き時間の読み取り権限のいずれかが許可されなかった場合、その接続は保存しません。既存の接続で期限切れ、権限不足、カレンダー単位の取得失敗が発生した場合は、再接続または対象カレンダーの変更を案内する API エラーを返します。診断ログにはトークン・カレンダー ID・Google のレスポンス本文を含めません。

Google OAuth の PKCE・state・Cookie による保護と、Cloud Console の診断確認については [OAuth セキュリティ](docs/oauth-security.md) を参照してください。

確定予定の書き込み先は、利用者ごとに作成する専用カレンダー「Vacaro」です。メインカレンダーへの書き込みは行いません。マイグレーション`000015`を適用し、Google Auth Platformのデータアクセスを`calendar.events`から`calendar.app.created`へ変更してください。既存の接続は専用カレンダーの管理許可を得るため再接続が必要です。Google上の旧予定は自動移動・削除しません。
