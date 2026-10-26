package store

import (
	"context"
	"net/url"
	"strings"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// CampaignLinks persists, per account and campaign, a claim the platform
// rejected until the user links an external account (Riot, Steam, ...).
// Rows live in account_campaign_links: linked=0 means the link is still
// needed, linked=1 means a later claim succeeded.
type CampaignLinks struct{ Q *gen.Queries }

func NewCampaignLinks(q *gen.Queries) *CampaignLinks { return &CampaignLinks{Q: q} }

// NeedsLink returns every pending link as accountID+":"+campaignID mapped to
// its link URL ("" when none was valid).
func (c *CampaignLinks) NeedsLink(ctx context.Context) (map[string]string, error) {
	rows, err := c.Q.ListUnlinkedCampaignLinks(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		out[r.AccountID+":"+r.CampaignID] = httpURL(r.LinkUrl)
	}
	return out, nil
}

// SetNeedsLink records that accountID must link before claiming campaignID.
// linkURL is kept only when it is an absolute http(s) URL.
func (c *CampaignLinks) SetNeedsLink(ctx context.Context, accountID, campaignID, linkURL string) error {
	return c.Q.UpsertAccountCampaignLink(ctx, gen.UpsertAccountCampaignLinkParams{
		AccountID: accountID, CampaignID: campaignID, Linked: 0, Checked: 1,
		LinkUrl: httpURL(linkURL), UpdatedAt: time.Now().Unix(),
	})
}

// ClearNeedsLink records that a claim for accountID and campaignID succeeded.
func (c *CampaignLinks) ClearNeedsLink(ctx context.Context, accountID, campaignID string) error {
	return c.Q.UpsertAccountCampaignLink(ctx, gen.UpsertAccountCampaignLinkParams{
		AccountID: accountID, CampaignID: campaignID, Linked: 1, Checked: 1,
		UpdatedAt: time.Now().Unix(),
	})
}

// httpURL returns raw when it is an absolute http or https URL with a host,
// else "".
func httpURL(raw string) string {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	return raw
}
