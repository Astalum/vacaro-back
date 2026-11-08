# Google OAuth の安全なフロー

Google Auth Platform の「安全なフローの使用」は、OAuth アプリの確認審査とは別の診断項目。公式ヘルプでは、インストール型アプリの PKCE、Android / Chrome の所有権確認、iOS の App Check、CSRF 対策の state を確認対象としている。警告だけではどのクライアント・項目が該当したかを特定できない。

## vacaro の実装

- Google ログインと Calendar 連携は、バックエンドでコードを交換する認可コード方式を使用する。
- 毎回独立したランダムな state と PKCE verifier を生成し、PKCE のチャレンジ方式に S256 を使用する。
- verifier は nonce や state と共有しない。認可 URL に verifier と client secret を含めない。
- state / verifier / nonce / 用途 / 利用者 ID / 有効期限を AES-GCM で暗号化した HttpOnly Cookie に保持する。本番は Secure / SameSite=Lax、有効期間は10分。
- コールバックの state と用途・有効期限を検証し、暗号化 Cookie に保持した verifier をサーバーからトークン交換時に送る。クエリで渡された verifier は使用しない。
- verifier の欠落・不正時は PKCE なしの交換に切り替えない。
- ログインでは従来の ID Token の署名・audience・nonce・認証済みメールアドレスの検証も行う。
- 既存のログインセッション・カレンダー接続は保持する。更新前に開始した未完了の認証は、最初からのやり直しが必要な場合がある。

## Google Cloud 側での確認

PKCE の追加だけで診断表示が消えるとは限らない。Web アプリに PKCE がなかったことだけでは、公式ヘルプが記載するインストール型アプリ向けの警告の原因とは断定できない。

本番クライアントが属するプロジェクトの Google Auth Platform で、Clients の一覧・クライアント種別と使用状況を確認する。インストール型やモバイル用の別クライアントが存在する場合は、そのクライアントの PKCE・所有権確認・App Check も対象になる。認可開始リクエストが response_type=code、state あり、PKCE S256 になっていることを確認する。Web 版は HTTPS のリダイレクト URI を登録し、クライアントシークレットをサーバーだけで保持する。

このリポジトリから Google Cloud Console の診断結果や全クライアントの設定を取得・変更する権限は確認できていない。診断の解消そのものは Console で別途確認する。確認審査・ブランド情報・ドメイン所有権・公開状態は、それぞれの該当画面で管理する。

## 検証

OAuth state の Cookie 往復、verifier の独立性・保護、PKCE S256 の認可 URL、トークン交換時の verifier、欠落・差し替え時の拒否、両ハンドラーから Cookie 内 verifier を使用することをテストする。Google アカウントで実際の認可を完了する操作と、Console の警告が消えたことの確認は自動テストに含まない。

## 公式資料

- [Google Auth Platform: Use secure flows](https://support.google.com/cloud/answer/15548748?hl=ja#use-secure-flows)
- [Web server OAuth](https://developers.google.com/identity/protocols/oauth2/web-server)
- [Go OAuth2 PKCE API](https://pkg.go.dev/golang.org/x/oauth2@v0.36.0#S256ChallengeOption)
