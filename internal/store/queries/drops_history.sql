-- name: ListRecentClaims :many
SELECT
  c.id           AS claim_id,
  c.account_id   AS account_id,
  a.display_name AS account_name,
  c.benefit_id   AS benefit_id,
  b.name         AS benefit_name,
  b.campaign_id  AS campaign_id,
  camp.name      AS campaign_name,
  camp.game      AS game,
  camp.platform  AS platform,
  c.claimed_at   AS claimed_at
FROM claims c
JOIN benefits b ON b.id = c.benefit_id
JOIN campaigns camp ON camp.id = b.campaign_id
JOIN accounts a ON a.id = c.account_id
ORDER BY c.claimed_at DESC
LIMIT ?;
