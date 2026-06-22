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
