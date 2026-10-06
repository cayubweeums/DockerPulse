export interface Host {
  id: string;
  name: string;
  driver: 'agent' | 'ssh' | 'socket';
  address: string;
  port: number;
  base_dir: string;
  status: 'online' | 'offline' | 'unknown';
  auth_token?: string;
  ssh_user?: string;
  ssh_key?: string;
  last_seen: string;
  created_at: string;
}

export interface ContainerPort {
  ip?: string;
  private_port: number;
  public_port?: number;
  type: string;
}

export interface ContainerInfo {
  id: string;
  names: string[];
  image: string;
  image_id: string;
  command: string;
  created: number;
  state: string; // "running", "exited", "paused"
  status: string; // e.g. "Up 2 hours"
  ports: ContainerPort[];
  stack?: string;
  service?: string;
  working_dir?: string;
  config_file?: string;
  cpu_pct: number;
  memory_mb: number;
  memory_pct: number;
  net_input_mb: number;
  net_output_mb: number;
  has_update?: boolean;
}

export interface Stack {
  id: string;
  host_id: string;
  name: string;
  path: string;
  status: string;
  auto_update: boolean;
  created_at: string;
  updated_at: string;
}

export interface StackRevision {
  id: string;
  stack_id: string;
  revision_num: number;
  compose_content: string;
  env_content: string;
  created_by: string;
  note: string;
  created_at: string;
}

export interface NetworkInfo {
  id: string;
  name: string;
  driver: string;
  scope: string;
  internal: boolean;
  subnet?: string;
  gateway?: string;
  containers: Record<string, string>;
}

export interface DiskUsageInfo {
  layers_size: number;
  images_size: number;
  containers_size: number;
  volumes_size: number;
  build_cache_size: number;
  dangling_images: number;
  unused_volumes: number;
}

export interface SystemInfo {
  hostname: string;
  os: string;
  kernel_version: string;
  docker_version: string;
  total_cpus: number;
  cpu_usage_pct: number;
  total_ram_bytes: number;
  used_ram_bytes: number;
}

export interface User {
  id: string;
  username: string;
  role: 'admin' | 'viewer';
  display_name?: string;
  avatar?: string;
  theme?: string;
}

export interface InAppNotification {
  id: string;
  title: string;
  message: string;
  type: 'update' | 'info' | 'warning' | 'error';
  host_id?: string;
  read: boolean;
  created_at: string;
}

export interface NotificationConfig {
  id: 'ntfy' | 'discord' | 'signal';
  enabled: boolean;
  config_json: string;
  status: 'unconfigured' | 'enabled' | 'error';
  last_error?: string;
  updated_at: string;
}

export interface SchedulerConfig {
  enabled: boolean;
  interval_minutes: number;
  last_run: string;
  next_run: string;
}

export interface ManagedUser {
  id: string;
  username: string;
  display_name: string;
  avatar?: string;
  theme?: string;
  role: 'admin' | 'viewer';
  created_at: string;
}
