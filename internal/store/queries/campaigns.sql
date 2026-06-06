-- name: UpsertCampaign :exec
INSERT INTO campaigns (id, platform, game, name, starts_at, ends_at, status, raw_json, discovered_at, kind, account_linked, account_link_url)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    name = excluded.name,
    starts_at = excluded.starts_at,
    ends_at = excluded.ends_at,
    status = excluded.status,
    raw_json = excluded.raw_json,
    kind = excluded.kind,
    account_linked = excluded.account_linked,
    account_link_url = excluded.account_link_url;

-- name: UpsertBenefit :exec
INSERT INTO benefits (id, campaign_id, name, required_minutes, image_url)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    name = excluded.name,
    required_minutes = excluded.required_minutes,
    image_url = excluded.image_url;

-- name: ListActiveCampaignsForPlatform :many
SELECT * FROM campaigns
WHERE platform = ? AND status = 'active' AND starts_at <= ? AND ends_at >= ?
ORDER BY discovered_at DESC;

-- name: ListBenefitsForCampaign :many
SELECT * FROM benefits WHERE campaign_id = ?;

-- name: ListClaimsForCampaign :many
-- Which accounts have claimed each benefit in a campaign. Powers the
-- per-account COLLECTED marks on the /drops expanded item list.
SELECT c.benefit_id, a.id AS account_id, a.login, a.platform, a.display_name
FROM claims c
JOIN accounts a ON a.id = c.account_id
JOIN benefits b ON b.id = c.benefit_id
WHERE b.campaign_id = ?;

-- name: GetCampaign :one
SELECT * FROM campaigns WHERE id = ?;

-- name: ListPastCampaigns :many
-- Campaigns that have ended. Whitelist filtering is applied in Go.
SELECT * FROM campaigns
WHERE ends_at < ?
ORDER BY ends_at DESC
LIMIT ?;

-- name: ListCurrentCampaigns :many
-- Campaigns currently in flight (starts_at <= now < ends_at).
-- Whitelist filtering is applied in Go.
SELECT * FROM campaigns
WHERE starts_at <= ? AND ends_at > ?
ORDER BY ends_at ASC
LIMIT ?;

-- name: ListUpcomingCampaigns :many
-- Campaigns announced but not yet started. Whitelist filtering is
-- applied in Go.
SELECT * FROM campaigns
WHERE starts_at > ?
ORDER BY starts_at ASC
LIMIT ?;
