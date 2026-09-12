# vacaro-front 画面・ルーティング設計

## 1. サービスの前提

vacaroは、主催者が候補枠を事前に列挙する日程調整サービスではない。

主催者は次の条件だけを設定する。

- 日程を調整する対象期間
- イベントに必要な時間
- 調整単位（イベント全体または小グループ）
- 回答期限

参加者は対象期間内で、自分が参加できる開始・終了日時を自由に複数登録する。サーバーは全参加者の参加可能時間を重ね合わせ、必要時間を満たす共通候補日時を算出する。

```text
主催者が調整期間と必要時間を設定
  → 招待URLを共有
    → 参加者が空いている時間帯を自由に登録
      → サーバーが全体または小グループごとに重なりを集計
        → 主催者がresults画面で開催日時を確定
```

時間区間はAPI仕様と同じく、開始を含み終了を含まない `[start_at, end_at)` として扱う。

## 2. 利用フロー

### 主催者

1. イベント名、調整対象期間、必要時間、回答期限を入力する。
2. 発行された招待URLを参加者へ共有する。
3. 自分の参加可能時間も登録する。
4. 小グループ調整の場合は、manage画面でグループを作成して参加者を割り当てる。
5. results画面で回答状況と、参加可能人数順に算出された共通候補を確認する。
6. 全体または小グループごとに開催日時を確定する。

### 参加者

1. 招待URLを開き、イベント内容を確認する。
2. 表示名を入力してイベントへ参加する。
3. 対象期間内で参加できる開始・終了日時を複数登録する。
4. 必要に応じてGoogleカレンダーから空き時間を取り込む。
5. 回答を送信し、共通候補と回答状況を確認する。

## 3. 画面一覧

| 区分 | 画面 | ルート | 主な役割 |
| --- | --- | --- | --- |
| 共通 | トップ | `/` | サービス説明、イベント作成への導線 |
| 共通 | ユーザー設定 | `/account` | Googleログイン、ログアウト、カレンダー連携・取り込み対象管理 |
| 主催者 | イベント作成 | `/events/new` | 調整対象期間と必要時間を設定 |
| 主催者 | 作成完了 | `/events/:eventId/created` | 招待URLを共有 |
| 共通 | イベント一覧 | `/events` | 自分が参加しているイベントを表示 |
| 招待 | 招待確認・参加 | `/invitations/:inviteToken` | イベント内容の確認と参加登録 |
| 参加者 | 参加可能時間入力 | `/events/:eventId/availability` | 自由な開始・終了日時を複数登録 |
| 参加者・主催者 | 集計結果・日程確定 | `/events/:eventId/results` | 全体またはグループ別の候補・回答を表示し、主催者は日時を確定 |
| 主催者 | イベント・グループ管理 | `/events/:eventId/manage` | 招待、イベント設定、小グループとメンバー編成を管理 |
| 主催者 | イベント編集 | `/events/:eventId/edit` | イベント条件を変更 |
| 共通 | 確定結果 | `/events/:eventId/confirmed` | 確定した日時を確認 |
| 共通 | Not Found | `*` | 無効なURLや存在しないイベントを案内 |

### 3.1 resultsとmanageの責務境界

| 操作 | results | manage |
| --- | :---: | :---: |
| 候補・回答結果の閲覧 | ○ | — |
| 全体・グループの日程確定／解除 | ○（主催者のみ） | — |
| グループ切り替え | ○ | — |
| グループ作成・編集・削除 | — | ○ |
| グループメンバー編成 | — | ○ |
| イベント編集・取消 | — | ○ |
| 招待URLの表示・コピー | — | ○ |

同じ情報や主要操作を両画面へ重複配置しない。画面間を移動するリンクには、具体的な目的を示す「このグループの日程を調整」「グループ編成を変更」などの文言を使用する。

## 4. 画面要件

### 4.1 イベント作成 `/events/new`

入力項目：

- イベント名（必須、1～200文字）
- 説明（任意、最大5,000文字）
- タイムゾーン（必須）
- 調整期間の開始日時 `window_start_at`（必須）
- 調整期間の終了日時 `window_end_at`（必須）
- イベントに必要な時間 `duration_minutes`（必須、5～1,440分）
- 調整単位 `scheduling_mode`（必須、`whole_event`または`groups`）
- 回答期限 `response_deadline_at`（任意）

この画面では個別の候補日時やグループを作成しない。`groups`を選んだ場合は作成完了画面からグループ管理への導線を強調する。作成成功後は `/events/:eventId/created` へ遷移する。

### 4.1.1 ユーザー設定 `/account`

- 未認証ユーザーとゲストユーザーにGoogleログインを案内
- ゲストで作成・参加済みのイベントはGoogleログイン後のユーザーへ引き継ぐ
- Google認証済みユーザーの表示名とメールアドレスを表示
- Googleカレンダーの接続・再接続・解除
- 空き時間の判定に使用するメイン・共有・サブカレンダーの選択
- セッションからのログアウト

Google OAuthの開始URLはAPIの`GET /api/v1/auth/google/start`を通常のブラウザ遷移で開く。Googleカレンダー接続後は`/account?google_calendar=connected`へ戻し、接続状況を連携カード内へ反映する。独立した接続完了ポップアップは表示しない。

### 4.2 作成完了 `/events/:eventId/created`

- イベント概要
- 調整対象期間と必要時間
- APIから返された `invite_url` の表示とコピー
- 管理画面、参加可能時間入力画面への導線

生の招待トークンは作成時と再発行時だけ取得できるため、再読み込み時の扱いを別途考慮する。

### 4.3 招待確認・参加 `/invitations/:inviteToken`

- 認証不要で公開招待情報を取得
- イベント名、説明、調整対象期間、必要時間、回答期限を表示
- 未認証なら表示名を入力してゲストセッションを作成
- イベント参加処理
- 参加後は `/events/:eventId/availability` へ遷移

無効な招待トークンまたは取消済みイベントは404として扱う。

### 4.4 参加可能時間入力 `/events/:eventId/availability`

- イベントの調整対象期間と必要時間を表示
- 週単位の時間カレンダー上で参加可能時間を色付きの帯として可視化
- カレンダーの空き部分を30分単位でドラッグして時間帯を直接追加し、右側の編集パネルから詳細を編集・削除
- Googleカレンダーから空き時間をプレビューして取り込む導線
- ユーザー設定で選択したカレンダーを判定対象として使用
- 空配列を「参加可能時間なし」として送信できる操作
- 既存回答がある場合は編集として表示

バリデーション：

- 各区間が調整対象期間内であること
- `start_at < end_at` であること
- 区間同士が重複しないこと
- 最大100区間であること

隣接する区間はAPI側で統合される。送信は `PUT /api/v1/events/:eventId/my-availability` により全件置換する。

### 4.5 集計結果 `/events/:eventId/results`

- 結果の表示と日程確定に責務を限定し、グループ編成やイベント設定を置かない
- `whole_event`ではイベント全体、`groups`では選択中グループの候補を表示
- グループ調整では、グループタブまたはセレクトで表示対象を切り替える
- グループ調整では「全体」タブを表示せず、URLの`group_id`検索パラメータで選択状態を共有可能にする
- `group_id`が未指定なら作成日時が最も古いグループを選択する
- 存在しない、または別イベントの`group_id`は404にせず、先頭グループへ置き換える
- 対象の必要時間を満たす共通候補を参加可能人数順に表示
- 共通候補を月間カレンダー上の日付・時間として表示
- 月をまたぐ調整期間は前月・次月ボタンで切り替え
- カレンダー上の候補を選択すると参加可能なメンバーを表示
- 各候補の開始・終了日時、参加可能人数、対象参加者を表示
- `whole_event`では全参加者、`groups`では選択中グループのメンバーについて、未回答者を含む回答状況を表示
- 日別タイムライン上で対象参加者の参加可能時間を表示
- 自分の回答を編集する導線
- 主催者には、選択した候補で全体またはグループの日程を確定・変更・解除する操作を表示
- 小グループが0件の場合は、候補画面の代わりにmanage画面でグループを作成する導線を表示
- グループのメンバーが0人の場合は、候補なしとしてメンバー割り当てへの導線を表示

候補は全体表示では `GET /api/v1/events/:eventId/candidates`、グループ表示では `GET /api/v1/events/:eventId/groups/:groupId/candidates` の結果を表示し、フロント側で独自再計算しない。全体表示と各グループ表示のどちらからも日時を確定できる。日時確定はresults画面のRoute Actionから対応するconfirmation APIへ送信し、成功後にローダーデータを再検証する。

### 4.6 イベント管理 `/events/:eventId/manage`

- イベント条件、調整単位、参加者数を表示
- 招待URLの表示・コピー
- イベント編集、取消
- `groups`モードでは小グループの作成、名称・必要時間の変更、削除
- グループ作成時または編集時に、参加者一覧からメンバーを複数選択して保存
- 参加者は複数グループへ所属可能として表示
- どのグループにも所属していない参加者を明示
- 各グループカードに、メンバー数、回答数、確定状態を表示
- 各グループカードから `/events/:eventId/results?group_id=:groupId` へ遷移
- 確定済みグループの必要時間、メンバー編成、削除操作は無効化し理由を表示
- `whole_event`モードでは最初のグループ作成フォームを表示する。作成時に`groups`へ切り替え、続けてグループと選択メンバーを登録する

manage画面では候補ランキング、詳細な回答結果、日程確定操作を表示しない。これらはresults画面へ集約する。

### 4.7 イベント編集 `/events/:eventId/edit`

- イベント名、説明、タイムゾーン
- 調整対象期間
- 必要時間
- 調整単位（確定日時がなく、`groups`から戻す場合はグループが0件のときだけ変更可能）
- 回答期限

変更後の期間が登録済みの参加可能時間を除外する場合、APIは422を返すため、影響を説明するエラーを表示する。

### 4.8 確定結果 `/events/:eventId/confirmed`

- `whole_event`では確定した開始・終了日時
- `groups`では全グループの名称、メンバー、確定した開始・終了日時
- イベント名と説明
- 参加者一覧
- Googleカレンダーへ追加する導線

## 5. ルーティング

APIの招待URLと認証境界に合わせ、公開招待と参加後のイベント画面を分離する。

```ts
import { type RouteConfig, index, route } from "@react-router/dev/routes";

export default [
  index("routes/home.tsx"),
  route("account", "routes/account.tsx"),
  route("events", "routes/events/index.tsx"),
  route("events/new", "routes/events/new.tsx"),
  route("events/:eventId/created", "routes/events/created.tsx"),
  route("events/:eventId/availability", "routes/events/availability.tsx"),
  route("events/:eventId/results", "routes/events/results.tsx"),
  route("events/:eventId/manage", "routes/events/manage.tsx"),
  route("events/:eventId/edit", "routes/events/edit.tsx"),
  route("events/:eventId/confirmed", "routes/events/confirmed.tsx"),
  route("invitations/:inviteToken", "routes/invitations/join.tsx"),
  route("*", "routes/not-found.tsx"),
] satisfies RouteConfig;
```

## 6. 画面とAPIの対応

| 画面 | 主なAPI |
| --- | --- |
| ユーザー設定 | `GET /api/v1/me`、`GET /api/v1/auth/google/start`、`DELETE /api/v1/session`、Googleカレンダー連携API |
| イベント作成 | `POST /api/v1/events` |
| 作成完了 | 作成レスポンスの `invite_url` |
| 招待確認 | `GET /api/v1/invitations/:inviteToken` |
| イベント参加 | `POST /api/v1/guest-sessions`、`POST /api/v1/invitations/:inviteToken/participants` |
| 参加可能時間入力 | `GET` / `PUT /api/v1/events/:eventId/my-availability` |
| カレンダー取込 | `POST /api/v1/events/:eventId/google-calendar-availability/preview` |
| 集計結果 | `GET /api/v1/events/:eventId/availability`、`GET /api/v1/events/:eventId/candidates` |
| グループ別集計結果 | `GET /api/v1/events/:eventId/groups`、グループ別availability・candidates・confirmation API |
| イベント管理 | `GET /api/v1/events/:eventId/participants`、グループCRUD・メンバー一括置換API |
| イベント編集 | `PATCH /api/v1/events/:eventId` |
| 確定結果 | `GET /api/v1/events/:eventId` |

## 7. MVPの実装範囲

1. イベント作成
2. 作成完了と招待URL共有
3. 招待確認・ゲスト参加
4. 参加可能時間の手動入力・編集
5. 共通候補と回答状況の表示
6. 主催者による日時確定
7. 小グループの作成・メンバー編成
8. 小グループ別の候補表示・日時確定

Google認証、Googleカレンダー連携、イベント一覧まで実装済み。別イベントからの回答コピーはMVP後でもよい。

## 8. イベント状態

現在のAPI仕様に合わせ、イベント状態は次の3つとする。

| 状態 | 説明 | 参加・回答・編集 |
| --- | --- | --- |
| `open` | 回答受付中 | 可能 |
| `confirmed` | 全体、またはすべての小グループの開催日時が確定済み | 不可。確定結果を表示 |
| `cancelled` | 取消済み | 不可。中止案内を表示 |

`draft` と `closed` は現在のAPIには存在しない。回答期限超過時の入力可否は、APIの実装と合わせて判定する。

`groups`モードでは一部グループだけが確定している間もイベントは`open`とする。1件以上存在するすべてのグループが確定した時点で`confirmed`へ遷移し、いずれかの確定を解除した場合は`open`へ戻す。

## 9. エラー・例外時の扱い

- セッションがない場合はゲストセッション作成またはログインへ誘導する。
- イベント未参加者がイベントIDの画面へアクセスした場合は404として扱う。
- `event_not_open` では回答受付が終了したことを案内する。
- 重複区間や期間外の入力は該当入力行の近くに表示する。
- 回答保存に失敗しても入力内容を保持し、再試行できるようにする。
- 確定済みイベントでは参加可能時間入力より確定結果を優先する。
