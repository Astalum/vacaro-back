# vacaro-back API仕様書

## 1. 文書情報

| 項目 | 内容 |
|---|---|
| 対象API | vacaro-back HTTP API |
| APIバージョン | v1 |
| ベースパス | `/api/v1` |
| 仕様更新日 | 2026-09-04 |
| 実装 | Go `net/http` |
| 認証 | 暗号化されたHttpOnly Cookie |

本書は現在のサーバー実装を基準としたAPI契約である。DB構造については[データベース設計書](database-design.md)を参照する。

## 2. 共通仕様

### 2.1 通信・データ形式

| 項目 | 仕様 |
|---|---|
| 本番プロトコル | HTTPS |
| リクエスト・レスポンス | JSON |
| Content-Type | `application/json` |
| JSONフィールド名 | `snake_case` |
| 日時 | RFC 3339。レスポンスはUTCの`Z`表記 |
| ID | UUID文字列 |
| 文字コード | UTF-8 |
| 時間区間 | 開始を含み終了を含まない`[start_at, end_at)` |
| JSONボディ上限 | 1 MiB |

JSONリクエストでは未知のフィールドを許可しない。また、1つのJSONオブジェクトの後に別の値が続くリクエストを拒否する。

### 2.2 認証

認証が必要なAPIでは次のCookieを送信する。

```http
Cookie: vacaro_session=<encrypted-value>
```

Cookie属性：

- `HttpOnly`
- `SameSite=Lax`
- `Path=/`
- 本番環境では`Secure`
- ゲストの標準有効期間は168時間
- Google認証済みユーザーの標準有効期間は720時間

本書の認証区分は次の意味を持つ。

| 区分 | 条件 |
|---|---|
| 不要 | セッションなしで利用可能 |
| 必要 | 有効なゲストまたはGoogle認証済みセッション |
| 参加者 | 対象イベントの`participants`に現在ユーザーが存在 |
| 主催者 | 対象イベントの`created_by_user_id`が現在ユーザーと一致 |
| Google認証 | `auth_accounts`に現在ユーザーのGoogleアカウントが存在 |
| Calendar連携 | Google認証済みでRefresh Tokenを保存済み |

### 2.3 リクエストID

すべてのレスポンスに`X-Request-ID`を付ける。クライアントが128文字以下の`X-Request-ID`を送った場合はその値を引き継ぎ、それ以外はサーバーが生成する。

### 2.4 成功レスポンス

通常の成功レスポンス：

```json
{
  "data": {}
}
```

一覧レスポンス：

```json
{
  "data": [],
  "meta": {}
}
```

返却データがない場合は`204 No Content`とし、ボディを返さない。ヘルスチェックとOAuthリダイレクトはこの形式の例外とする。

### 2.5 エラーレスポンス

```json
{
  "error": {
    "code": "validation_failed",
    "message": "入力内容を確認してください",
    "fields": {
      "title": "1～200文字で指定してください"
    },
    "request_id": "c4c218ac9ac85af0d8fb7250e25c5354"
  }
}
```

`fields`はフィールド単位のエラーがない場合に`null`となることがある。認証ミドルウェアの`401`や`invalid_json`では`fields`を含めない。

| HTTP Status | code | 条件 |
|---:|---|---|
| `400` | `invalid_json` | JSON不正、未知フィールド、複数JSON値 |
| `400` | `oauth_failed` | OAuthコールバック、ID Token、Google API処理の失敗 |
| `401` | `authentication_required` | セッションなし、改ざん、期限切れ |
| `403` | `permission_denied` | 主催者限定操作などの権限不足 |
| `403` | `google_authentication_required` | 回答コピーでGoogle認証がない |
| `404` | `resource_not_found` | リソースなし、イベントに未参加、無効な招待トークン |
| `409` | `already_joined` | 同一イベントへ参加済み |
| `409` | `event_not_open` | 受付中でないイベントへの更新 |
| `409` | `organizer_cannot_leave` | 主催者によるイベント退出 |
| `409` | `invalid_event_transition` | 許可されていない状態遷移 |
| `409` | `calendar_not_connected` | Googleカレンダー未連携 |
| `422` | `validation_failed` | 業務入力規則への違反 |
| `500` | `internal_error` | 予期しない内部エラー |
| `503` | `google_not_configured` | サーバー側のGoogle連携設定が無効 |

## 3. リソース表現

### 3.1 User

```json
{
  "id": "0198d540-45b8-7f91-9fa8-4d7a455b1022",
  "account_type": "guest",
  "display_name": "田中",
  "email": null,
  "guest_expires_at": "2026-09-11T03:00:00Z",
  "created_at": "2026-09-04T03:00:00Z",
  "updated_at": "2026-09-04T03:00:00Z"
}
```

| フィールド | 型 | 説明 |
|---|---|---|
| `account_type` | string | `guest`または`registered` |
| `email` | string/null | Google認証済みユーザーのメールアドレス |
| `guest_expires_at` | string/null | ゲストの失効日時。登録ユーザーは`null` |

### 3.2 Event

```json
{
  "id": "0198d543-e7f0-7df2-95fa-cee649624cef",
  "created_by_user_id": "0198d540-45b8-7f91-9fa8-4d7a455b1022",
  "title": "開発ミーティング",
  "description": "次回リリースについて相談します",
  "time_zone": "Asia/Tokyo",
  "window_start_at": "2026-09-10T00:00:00Z",
  "window_end_at": "2026-09-12T12:00:00Z",
  "duration_minutes": 60,
  "status": "open",
  "confirmed_start_at": null,
  "confirmed_end_at": null,
  "response_deadline_at": "2026-09-09T12:00:00Z",
  "created_at": "2026-09-04T03:00:00Z",
  "updated_at": "2026-09-04T03:00:00Z"
}
```

`status`は`open`、`confirmed`、`cancelled`のいずれか。公開招待APIでは`created_by_user_id`を返さない。

### 3.3 Participant

```json
{
  "id": "0198d545-6200-7973-8609-ab29d9ab461a",
  "event_id": "0198d543-e7f0-7df2-95fa-cee649624cef",
  "user_id": "0198d540-45b8-7f91-9fa8-4d7a455b1022",
  "display_name": "田中",
  "role": "member",
  "response_status": "submitted",
  "created_at": "2026-09-04T03:10:00Z",
  "updated_at": "2026-09-04T03:20:00Z"
}
```

`role`は`organizer`または`member`、`response_status`は`pending`または`submitted`。

### 3.4 AvailabilityInterval

```json
{
  "id": "0198d547-3c18-76ee-b85c-e99897199aef",
  "participant_id": "0198d545-6200-7973-8609-ab29d9ab461a",
  "start_at": "2026-09-10T01:00:00Z",
  "end_at": "2026-09-10T03:00:00Z",
  "source": "manual",
  "created_at": "2026-09-04T03:20:00Z",
  "updated_at": "2026-09-04T03:20:00Z"
}
```

`source`は`manual`または`google_calendar`。Googleカレンダーのプレビュー応答ではID・参加者ID・作成日時を返さない。

## 4. エンドポイント一覧

| Method | Path | 認証 | 成功 |
|---|---|---|---:|
| `GET` | `/health` | 不要 | `200` |
| `GET` | `/ready` | 不要 | `200` |
| `POST` | `/api/v1/guest-sessions` | 不要 | `201` |
| `GET` | `/api/v1/me` | 必要 | `200` |
| `PATCH` | `/api/v1/me` | 必要 | `200` |
| `DELETE` | `/api/v1/session` | 必要 | `204` |
| `POST` | `/api/v1/events` | 必要 | `201` |
| `GET` | `/api/v1/events` | 必要 | `200` |
| `GET` | `/api/v1/events/{event_id}` | 参加者 | `200` |
| `PATCH` | `/api/v1/events/{event_id}` | 主催者 | `200` |
| `DELETE` | `/api/v1/events/{event_id}` | 主催者 | `204` |
| `POST` | `/api/v1/events/{event_id}/invite-token` | 主催者 | `200` |
| `PUT` | `/api/v1/events/{event_id}/confirmation` | 主催者 | `200` |
| `DELETE` | `/api/v1/events/{event_id}/confirmation` | 主催者 | `200` |
| `GET` | `/api/v1/invitations/{invite_token}` | 不要 | `200` |
| `POST` | `/api/v1/invitations/{invite_token}/participants` | 必要 | `201` |
| `GET` | `/api/v1/events/{event_id}/participants` | 参加者 | `200` |
| `DELETE` | `/api/v1/events/{event_id}/participants/me` | 一般参加者 | `204` |
| `GET` | `/api/v1/events/{event_id}/my-availability` | 参加者 | `200` |
| `PUT` | `/api/v1/events/{event_id}/my-availability` | 参加者 | `200` |
| `POST` | `/api/v1/events/{event_id}/my-availability/copy` | Google認証・参加者 | `200` |
| `GET` | `/api/v1/events/{event_id}/availability` | 参加者 | `200` |
| `GET` | `/api/v1/events/{event_id}/candidates` | 参加者 | `200` |
| `GET` | `/api/v1/auth/google/start` | 不要 | `303` |
| `GET` | `/api/v1/auth/google/callback` | 不要 | `303` |
| `POST` | `/api/v1/integrations/google-calendar` | Google認証 | `200` |
| `GET` | `/api/v1/integrations/google-calendar/calendars` | Google認証・Calendar連携 | `200` |
| `GET` | `/api/v1/integrations/google-calendar/callback` | Google認証 | `303` |
| `DELETE` | `/api/v1/integrations/google-calendar` | Google認証・Calendar連携 | `204` |
| `POST` | `/api/v1/events/{event_id}/google-calendar-availability/preview` | 参加者・Calendar連携 | `200` |

## 5. システム

### 5.1 Liveness

```http
GET /health
```

DBへ接続せず、APIプロセスの生存を確認する。

```json
{"status":"ok"}
```

### 5.2 Readiness

```http
GET /ready
```

2秒以内にDBへ接続できた場合は`200`を返す。

```json
{"status":"ok"}
```

接続できない場合は`503`を返す。

```json
{"status":"unavailable"}
```

## 6. セッション・ユーザー

### 6.1 ゲストセッション作成

```http
POST /api/v1/guest-sessions
```

Request：

```json
{"display_name":"田中"}
```

- `display_name`: 前後空白を除いて1～100文字
- ゲストユーザーとセッションCookieを作成
- Response: `201`、`data`はUser

### 6.2 自分の情報取得

```http
GET /api/v1/me
```

Response: `200`、`data`はUser。

### 6.3 表示名変更

```http
PATCH /api/v1/me
```

```json
{"display_name":"新しい表示名"}
```

Response: `200`、`data`は更新後のUser。

### 6.4 ログアウト

```http
DELETE /api/v1/session
```

セッションCookieを削除し、`204`を返す。

## 7. イベント

### 7.1 イベント作成

```http
POST /api/v1/events
```

```json
{
  "title": "開発ミーティング",
  "description": "次回リリースについて相談します",
  "time_zone": "Asia/Tokyo",
  "window_start_at": "2026-09-10T00:00:00Z",
  "window_end_at": "2026-09-12T12:00:00Z",
  "duration_minutes": 60,
  "response_deadline_at": "2026-09-09T12:00:00Z"
}
```

入力規則：

- `title`: 必須、1～200文字
- `description`: 任意、最大5,000文字。空白だけの場合は`null`として保存
- `time_zone`: 必須、有効なIANAタイムゾーン
- `window_start_at`: 必須
- `window_end_at`: 必須、`window_start_at`より後
- `duration_minutes`: 5～1,440分かつイベント期間以下
- `response_deadline_at`: 任意、`window_end_at`以前

作成者は同じトランザクションで主催者として参加する。Response: `201`。

```json
{
  "data": {
    "id": "0198d543-e7f0-7df2-95fa-cee649624cef",
    "status": "open",
    "invite_token": "<random-token>",
    "invite_url": "http://localhost:3000/invitations/<random-token>"
  }
}
```

実際のレスポンスにはEventの全フィールドも含む。生の招待トークンは作成時と再発行時だけ返す。

### 7.2 イベント一覧

```http
GET /api/v1/events?status=open&limit=20&cursor=<opaque-cursor>
```

| Query | 省略時 | 制約 |
|---|---:|---|
| `status` | 全状態 | `open`、`confirmed`、`cancelled` |
| `limit` | `20` | 最大`100`。0以下や数値以外も`20` |
| `cursor` | 先頭 | レスポンスで得た値をそのまま指定 |

現在ユーザーが参加しているイベントを`created_at DESC, id DESC`で返す。

```json
{
  "data": [],
  "meta": {"next_cursor": null}
}
```

### 7.3 イベント詳細

```http
GET /api/v1/events/{event_id}
```

Response: `200`、`data`はEvent。未参加の場合は`404`。

### 7.4 イベント更新

```http
PATCH /api/v1/events/{event_id}
```

主催者かつ`open`状態の場合のみ更新できる。すべてのフィールドは任意。

```json
{
  "title": "更新後タイトル",
  "description": null,
  "time_zone": "Asia/Tokyo",
  "window_start_at": "2026-09-10T00:00:00Z",
  "window_end_at": "2026-09-11T12:00:00Z",
  "duration_minutes": 90,
  "response_deadline_at": null
}
```

`description`と`response_deadline_at`へ`null`を送ると値を削除する。変更後の期間が登録済み参加可能時間を除外する場合は`422`。Response: `200`、更新後のEvent。

### 7.5 イベント取消

```http
DELETE /api/v1/events/{event_id}
```

イベントを物理削除せず`cancelled`にし、`204`を返す。既に取消済みの場合も`204`。

### 7.6 招待トークン再発行

```http
POST /api/v1/events/{event_id}/invite-token
```

主催者かつ`open`状態で利用できる。以前のトークンは直ちに無効になる。Response: `200`、Eventに新しい`invite_token`と`invite_url`を加えた形式。

### 7.7 日程確定

```http
PUT /api/v1/events/{event_id}/confirmation
```

```json
{
  "start_at": "2026-09-10T01:00:00Z",
  "end_at": "2026-09-10T02:00:00Z"
}
```

- イベント期間内であること
- `end_at - start_at`が`duration_minutes`と一致すること
- 主催者のみ実行可能

Response: `200`、`status=confirmed`のEvent。

### 7.8 確定解除

```http
DELETE /api/v1/events/{event_id}/confirmation
```

`confirmed`から`open`へ戻し、確定日時を`null`にする。Response: `200`、更新後のEvent。

## 8. 招待・参加者

### 8.1 公開招待情報

```http
GET /api/v1/invitations/{invite_token}
```

Response: `200`、`created_by_user_id`を除いたEvent。無効なトークンまたは取消済みイベントは`404`。

### 8.2 イベント参加

```http
POST /api/v1/invitations/{invite_token}/participants
```

```json
{"display_name":"田中"}
```

- `display_name`: 前後空白を除いて1～100文字
- イベントは`open`であること
- 同じユーザーが参加済みの場合は`409 already_joined`
- Response: `201`、`data`は作成されたParticipant

### 8.3 参加者一覧

```http
GET /api/v1/events/{event_id}/participants
```

Response: `200`、`data`はParticipant配列。主催者を先頭に、作成日時順で返す。

### 8.4 イベント退出

```http
DELETE /api/v1/events/{event_id}/participants/me
```

一般参加者をイベントから削除し、回答も同時に削除する。主催者は退出できず`409 organizer_cannot_leave`。成功時は`204`。

## 9. 参加可能時間・候補

### 9.1 自分の回答取得

```http
GET /api/v1/events/{event_id}/my-availability
```

```json
{
  "data": {
    "response_status": "submitted",
    "intervals": []
  }
}
```

`intervals`は開始日時順のAvailabilityInterval配列。

### 9.2 自分の回答を一括置換

```http
PUT /api/v1/events/{event_id}/my-availability
```

```json
{
  "intervals": [
    {
      "start_at": "2026-09-10T01:00:00Z",
      "end_at": "2026-09-10T03:00:00Z"
    }
  ]
}
```

- 最大100区間
- イベントは`open`であること
- 各区間はイベント期間内かつ`start_at < end_at`
- 重複区間は`422`
- 隣接区間は統合
- 既存回答をすべて置換
- 空配列は「参加可能時間なし」という提出済み回答

Response: `200`。

```json
{
  "data": {
    "response_status": "submitted",
    "intervals": []
  }
}
```

### 9.3 別イベントから回答をコピー

```http
POST /api/v1/events/{target_event_id}/my-availability/copy
```

```json
{"source_event_id":"0198d543-e7f0-7df2-95fa-cee649624cef"}
```

- Google認証必須
- コピー元とコピー先の両方に参加していること
- コピー元とコピー先は別イベントであること
- コピー元の回答が`submitted`であること
- コピー先は`open`であること
- コピー先期間と重なる絶対日時だけを切り取ってコピー
- コピー先の既存回答は維持し、重複・隣接区間を統合
- コピー元に区間があるが重なる区間がない場合は`422`

時刻だけを別日へ移動する機能ではない。Response: `200`、一括置換と同じ形式。

### 9.4 全参加者の回答取得

```http
GET /api/v1/events/{event_id}/availability
```

```json
{
  "data": [
    {
      "participant": {},
      "intervals": []
    }
  ]
}
```

Response: `200`。参加者ごとにParticipantとAvailabilityInterval配列を返す。

### 9.5 共通候補日時

```http
GET /api/v1/events/{event_id}/candidates?limit=20
```

```json
{
  "data": [
    {
      "start_at": "2026-09-10T01:00:00Z",
      "end_at": "2026-09-10T02:00:00Z",
      "available_participant_count": 2,
      "total_participant_count": 3,
      "available_participant_ids": ["0198d545-6200-7973-8609-ab29d9ab461a"]
    }
  ],
  "meta": {"limit": 20}
}
```

イベントの`duration_minutes`を満たす開始候補を参加可能人数の降順、同数なら開始日時の昇順で返す。未回答者は`total_participant_count`には含むが参加可能人数には含めない。`limit`は標準20、最大100。

## 10. Google認証・Calendar連携

Google関連の環境変数がすべて未設定の場合、Google以外のAPIは通常どおり利用できる。Google OAuth・Calendar APIは`503 google_not_configured`を返す。

### 10.1 Googleログイン開始

```http
GET /api/v1/auth/google/start
```

暗号化した10分間有効のOAuth state Cookieを設定し、Google認可画面へ`303 See Other`でリダイレクトする。ゲストセッションがあれば、そのユーザーIDをstateへ関連付ける。

### 10.2 Googleログインコールバック

```http
GET /api/v1/auth/google/callback?code=<code>&state=<state>
```

次を検証する。

- state Cookie、state値、有効期限、用途
- ID Tokenの署名・issuer・audience・有効期限
- nonce
- `email_verified`

Googleの`sub`をアカウント識別子として使用し、メールアドレスだけでは同一人物と判定しない。ゲストから開始した場合は、そのゲストが所有・参加するイベントをGoogleユーザーへ引き継ぐ。

成功時は登録ユーザー用Cookieを設定し、次へ`303`でリダイレクトする。

```text
{PUBLIC_APP_URL}?google_login=success
```

### 10.3 Googleカレンダー連携開始

```http
POST /api/v1/integrations/google-calendar
```

Google認証済みセッションが必要。Calendar FreeBusy権限とカレンダー一覧の読み取り権限を求める認可URLを返す。

```json
{
  "data": {
    "authorization_url": "https://accounts.google.com/o/oauth2/auth?..."
  }
}
```

### 10.4 Googleカレンダーコールバック

```http
GET /api/v1/integrations/google-calendar/callback?code=<code>&state=<state>
```

開始時と同じ登録ユーザーのセッションとstate Cookieが必要。取得したRefresh TokenはAES-GCMで暗号化してDBへ保存する。

成功時は次へ`303`でリダイレクトする。

```text
{PUBLIC_APP_URL}?google_calendar=connected
```

### 10.5 Googleカレンダー一覧

```http
GET /api/v1/integrations/google-calendar/calendars
```

空き時間の判定対象として選択できるカレンダーを返す。予定のタイトルや内容は返さない。

```json
{
  "data": {
    "calendars": [
      {
        "id": "user@example.com",
        "summary": "メインカレンダー",
        "primary": true,
        "selected": true,
        "background_color": "#4285f4"
      }
    ]
  }
}
```

### 10.6 Googleカレンダー連携解除

```http
DELETE /api/v1/integrations/google-calendar
```

Google側でRefresh Tokenを失効させた後、DBの暗号文を削除する。成功時は`204`。未連携の場合は`409 calendar_not_connected`。

### 10.7 空き時間プレビュー

```http
POST /api/v1/events/{event_id}/google-calendar-availability/preview
```

```json
{"calendar_ids":["primary"]}
```

- `calendar_ids`: 省略または空配列なら`["primary"]`
- 最大50件
- 空文字列は不可
- 対象イベントへの参加とCalendar連携が必要

イベント期間をGoogle Calendar FreeBusy APIへ問い合わせ、予定あり区間を除いた時間を返す。予定のタイトル・説明・参加者は取得・返却しない。このAPIはプレビュー専用でDBへ保存しない。

```json
{
  "data": {
    "intervals": [
      {
        "start_at": "2026-09-10T01:00:00Z",
        "end_at": "2026-09-10T02:00:00Z",
        "source": "google_calendar"
      }
    ]
  }
}
```

## 11. イベント状態遷移

```mermaid
stateDiagram-v2
    [*] --> open: 作成
    open --> confirmed: 日程確定
    confirmed --> open: 確定解除
    open --> cancelled: 取消
    confirmed --> cancelled: 取消
```

| 現在 | 操作 | 結果 |
|---|---|---|
| `open` | 参加・回答・内容更新 | 許可 |
| `open` | 確定 | `confirmed` |
| `open` | 取消 | `cancelled` |
| `confirmed` | 確定解除 | `open` |
| `confirmed` | 取消 | `cancelled` |
| `confirmed` | 参加・回答・内容更新 | 拒否 |
| `cancelled` | 再度取消 | `cancelled`のまま成功 |
| `cancelled` | その他の変更 | 拒否 |

## 12. 現在の制約と本番運用上の注意

- フロントエンドとAPIは同一サイト配下での利用を前提とする。
- CORSミドルウェア、Origin検証、CSRFトークンは現時点では未実装。
- APIレート制限は未実装。
- サーバー側セッションテーブルはなく、Cookieの個別即時失効や端末別管理は行わない。
- イベントIDなどのUUID形式をHandler層で事前検証していないため、不正形式へのエラー整形は今後の改善対象。
- 未定義パスの`404`と許可されていないHTTPメソッドの`405`は、現時点ではGo標準Muxのプレーンテキスト応答となる。
- Google OAuthの実環境動作にはGoogle Cloud ConsoleのOAuthクライアントと許可済みリダイレクトURIが必要。
- `SESSION_KEY`と`TOKEN_ENCRYPTION_KEY`は別々の32バイト鍵をBase64化し、Secret Manager等から供給する。
