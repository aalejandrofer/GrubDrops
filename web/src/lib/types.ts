export interface Telemetry {
  WatchTimeTotal: string;
  ClaimsTotal: number;
  ClaimsToday: number;
  ActiveCamps: number;
  Completed: number;
  TotalDrops: number;
  NextClaimETA: string;
  NextClaimName: string;
}

export interface MineCard {
  ID: string;
  Name: string;
  Platform: string;
  State: string;
  StateSub: string;
  Channel: string;
  DropName: string;
  DropPercent: number;
  DropETA: string;
  Enabled: boolean;
}

export interface MiningColumns {
  Twitch: MineCard[] | null;
  Kick: MineCard[] | null;
  KickWatchMode: string;
}

export interface DashAlert { Kind: string; Account: string; URL: string; Action: string; }

export interface DashCampaign {
  ID: string; Name: string; Platform: string; Game: string; Kind: string;
  Drops: number; Channels: number; EndsIn: string; EndsUrgent: boolean;
  Claimed: number; Total: number;
}

export interface DashLiveChannel {
  Login: string; Platform: string; URL: string; Initial: string;
  Game: string; Campaign: string; Views: string; ViewerN: number;
}

export interface DashEventField { Key: string; Value: string; }

export interface DashEvent {
  ID: string; Time: string; Kind: string; Color: string; BodyHTML: string;
  Account: string; Platform: string; Details: DashEventField[] | null;
}

export interface DashEventAccount { ID: string; Label: string; Platform: string; }

export interface DashboardSnapshot {
  Tele: Telemetry;
  Mining: MiningColumns;
  UpdatedAt: string;
  Uptime: string;
  NextClaims: MineCard[] | null;
  ActiveCamps: DashCampaign[] | null;
  LiveChannels: DashLiveChannel[] | null;
  Events: DashEvent[] | null;
  EventAccounts: DashEventAccount[] | null;
  Alerts: DashAlert[] | null;
}

export interface ApiErrorEnvelope {
  error: { code: string; message: string };
}

export interface AccountCampaignRow { ID: string; Name: string; Game: string; EndsIn: string; StartsIn: string; }
export interface AccountGameRow { Rank: number; Name: string; }

export interface AccountDetail {
  ID: string;
  Platform: string;
  DisplayName: string;
  Enabled: boolean;
  State: string;
  StateLabel: string;
  CurrentCampaign: string;
  CurrentGame: string;
  CurrentChannel: string;
  ProgressPct: number;
  WatchETA: string;
  Uptime: string;
  Games: AccountGameRow[] | null;
  EligibleCampaigns: AccountCampaignRow[] | null;
  UpcomingCampaigns: AccountCampaignRow[] | null;
}

export interface CampaignBenefit { ID: string; Name: string; RequiredMinutes: number; ImageURL: string; }
export interface CampaignDetail {
  ID: string; Name: string; Platform: string; Game: string; Status: string; Kind: string;
  StartsAt: string; EndsAt: string; EndsIn: string; EndsUrgent: boolean;
  Benefits: CampaignBenefit[] | null;
  EligibleAccounts: string[] | null; SourceAccounts: string[] | null;
  AccountLinked: boolean; AccountLinkURL: string;
}
