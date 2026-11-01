package store

import "context"

const keyNotifyRewardCodes = "settings:notify_reward_codes"

// NotifyRewardCodes reports whether new reward codes and their expiry
// reminders are sent. Default on.
func (s *Settings) NotifyRewardCodes(ctx context.Context) (bool, error) {
	return s.boolKey(ctx, keyNotifyRewardCodes, true)
}

func (s *Settings) SetNotifyRewardCodes(ctx context.Context, on bool) error {
	return s.setBool(ctx, keyNotifyRewardCodes, on)
}
