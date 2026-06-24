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
  Enabled: boolean;
}

export interface MiningColumns {
  Twitch: MineCard[] | null;
  Kick: MineCard[] | null;
  KickWatchMode: string;
}

export interface DashboardSnapshot {
  Tele: Telemetry;
  Mining: MiningColumns;
  UpdatedAt: string;
  Uptime: string;
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
