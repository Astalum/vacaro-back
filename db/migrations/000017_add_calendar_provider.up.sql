CREATE TABLE user_calendar_preferences (
 user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 provider TEXT NOT NULL CHECK(provider IN ('none','google','icloud','timetree')),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO user_calendar_preferences(user_id,provider)
SELECT u.id, CASE
 WHEN EXISTS(SELECT 1 FROM google_calendar_connections g JOIN auth_accounts a ON a.id=g.auth_account_id WHERE a.user_id=u.id)
  AND EXISTS(SELECT 1 FROM icloud_calendar_connections i WHERE i.user_id=u.id) THEN 'none'
 WHEN EXISTS(SELECT 1 FROM google_calendar_connections g JOIN auth_accounts a ON a.id=g.auth_account_id WHERE a.user_id=u.id) THEN 'google'
 WHEN EXISTS(SELECT 1 FROM icloud_calendar_connections i WHERE i.user_id=u.id) THEN 'icloud'
 ELSE 'none' END FROM users u;
