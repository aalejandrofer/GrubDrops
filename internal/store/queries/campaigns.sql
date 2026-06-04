-- name: UpsertCampaign :exec
INSERT INTO campaigns (id, platform, game, name, starts_at, ends_at, status, raw_json, discovered_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    name = excluded.name,
    starts_at = excluded.starts_at,
    ends_at = excluded.ends_at,
    status = excluded.status,
    raw_json = excluded.raw_json;

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
