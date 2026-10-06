import { Host, ContainerInfo, Stack, StackRevision, NetworkInfo, DiskUsageInfo, SystemInfo, User } from '../types';

const API_BASE = '/api';

function getAuthHeader(): Record<string, string> {
  const token = localStorage.getItem('dockpulse_token');
  return token ? { Authorization: `Bearer ${token}` } : {};
}

async function request<T>(endpoint: string, options: RequestInit = {}): Promise<T> {
  const res = await fetch(`${API_BASE}${endpoint}`, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      ...getAuthHeader(),
      ...(options.headers || {}),
    },
  });

  if (!res.ok) {
    const errorData = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(errorData.error || `HTTP error ${res.status}`);
  }

  return res.json();
}

export const api = {
  // Auth
  getAuthStatus: () => request<{ initialized: boolean }>('/auth/status'),
  setup: (data: { username: string; password: string; base_dir?: string }) =>
    request<{ token: string; user: User }>('/auth/setup', {
      method: 'POST',
      body: JSON.stringify(data),
    }),
  login: (data: { username: string; password: string }) =>
    request<{ token: string; user: User }>('/auth/login', {
      method: 'POST',
      body: JSON.stringify(data),
    }),
  getMe: () => request<User>('/auth/me'),

  // Hosts
  listHosts: () => request<Host[]>('/hosts'),
  createHost: (data: Partial<Host> & { ssh_key?: string; auth_token?: string }) =>
    request<Host>('/hosts', {
      method: 'POST',
      body: JSON.stringify(data),
    }),
  updateHost: (id: string, data: { name?: string; base_dir?: string }) =>
    request<Host>(`/hosts/${id}`, {
      method: 'PUT',
      body: JSON.stringify(data),
    }),
  getHost: (id: string) => request<Host>(`/hosts/${id}`),
  deleteHost: (id: string) => request<{ message: string }>(`/hosts/${id}`, { method: 'DELETE' }),
  getHostSystem: (id: string) => request<SystemInfo>(`/hosts/${id}/system`),

  // Containers
  listContainers: (hostId: string) => request<ContainerInfo[]>(`/hosts/${hostId}/containers`),
  startContainer: (hostId: string, cid: string) =>
    request<{ success: boolean }>(`/hosts/${hostId}/containers/${cid}/start`, { method: 'POST' }),
  stopContainer: (hostId: string, cid: string) =>
    request<{ success: boolean }>(`/hosts/${hostId}/containers/${cid}/stop`, { method: 'POST' }),
  restartContainer: (hostId: string, cid: string) =>
    request<{ success: boolean }>(`/hosts/${hostId}/containers/${cid}/restart`, { method: 'POST' }),
  removeContainer: (hostId: string, cid: string, force = false) =>
    request<{ success: boolean }>(`/hosts/${hostId}/containers/${cid}?force=${force}`, {
      method: 'DELETE',
    }),

  // Stacks & Compose Files
  listStacks: (hostId: string) => request<Stack[]>(`/hosts/${hostId}/stacks`),
  discoverStacks: (hostId: string) => request<Stack[]>(`/hosts/${hostId}/stacks/discover`, { method: 'POST' }),
  getStackFiles: (hostId: string, stackId: string) =>
    request<{ compose: string; env: string; path: string }>(`/hosts/${hostId}/stacks/${stackId}/files`),
  saveStackFiles: (hostId: string, stackId: string, data: { compose: string; env: string; note?: string }) =>
    request<{ success: boolean }>(`/hosts/${hostId}/stacks/${stackId}/files`, {
      method: 'PUT',
      body: JSON.stringify(data),
    }),
  deleteStack: (hostId: string, stackId: string) =>
    request<{ success: boolean }>(`/hosts/${hostId}/stacks/${stackId}`, { method: 'DELETE' }),
  listStackRevisions: (hostId: string, stackId: string) =>
    request<StackRevision[]>(`/hosts/${hostId}/stacks/${stackId}/revisions`),

  // Networks
  listNetworks: (hostId: string) => request<NetworkInfo[]>(`/hosts/${hostId}/networks`),
  createNetwork: (hostId: string, name: string, driver = 'bridge') =>
    request<{ success: boolean }>(`/hosts/${hostId}/networks`, {
      method: 'POST',
      body: JSON.stringify({ name, driver }),
    }),
  removeNetwork: (hostId: string, nid: string) =>
    request<{ success: boolean }>(`/hosts/${hostId}/networks/${nid}`, { method: 'DELETE' }),
  connectNetwork: (hostId: string, nid: string, containerId: string) =>
    request<{ success: boolean }>(`/hosts/${hostId}/networks/${nid}/connect`, {
      method: 'POST',
      body: JSON.stringify({ container_id: containerId }),
    }),
  disconnectNetwork: (hostId: string, nid: string, containerId: string) =>
    request<{ success: boolean }>(`/hosts/${hostId}/networks/${nid}/disconnect`, {
      method: 'POST',
      body: JSON.stringify({ container_id: containerId }),
    }),

  // Storage & Prune
  getStorageUsage: (hostId: string) => request<DiskUsageInfo>(`/hosts/${hostId}/storage`),
  pruneStorage: (hostId: string, all = false) =>
    request<{ images_deleted: number; space_reclaimed: number }>(
      `/hosts/${hostId}/storage/prune?all=${all}`,
      { method: 'POST' }
    ),

  // Updates
  checkUpdates: (hostId: string) =>
    request<Array<{ image: string; current_digest: string; remote_digest: string; has_update: boolean }>>(
      `/hosts/${hostId}/updates/check`
    ),

  // Compose streaming action helper
  streamComposeAction: (
    hostId: string,
    stackId: string,
    action: string,
    onChunk: (chunk: string) => void,
    onDone: (err?: Error) => void
  ) => {
    const token = localStorage.getItem('dockpulse_token');
    fetch(`${API_BASE}/hosts/${hostId}/stacks/${stackId}/action`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        ...(token ? { Authorization: `Bearer ${token}` } : {}),
      },
      body: JSON.stringify({ action }),
    })
      .then(async (response) => {
        if (!response.ok) {
          throw new Error(`HTTP error ${response.status}`);
        }
        const reader = response.body?.getReader();
        const decoder = new TextDecoder();
        if (!reader) {
          onDone();
          return;
        }

        while (true) {
          const { done, value } = await reader.read();
          if (done) break;
          if (value) {
            onChunk(decoder.decode(value, { stream: true }));
          }
        }
        onDone();
      })
      .catch((err) => {
        onDone(err);
      });
  },

  // WebSocket URLs
  getLogWebSocketURL: (hostId: string, containerId: string, follow = true, tail = '200') => {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const host = window.location.host;
    const token = localStorage.getItem('dockpulse_token') || '';
    return `${protocol}//${host}/api/hosts/${hostId}/containers/${containerId}/logs?follow=${follow}&tail=${tail}&token=${token}`;
  },

  getTerminalWebSocketURL: (hostId: string, containerId: string) => {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const host = window.location.host;
    const token = localStorage.getItem('dockpulse_token') || '';
    return `${protocol}//${host}/api/hosts/${hostId}/containers/${containerId}/terminal?token=${token}`;
  },

  // In-App Notifications
  listNotifications: (limit = 100) => request<import('../types').InAppNotification[]>(`/notifications?limit=${limit}`),
  getUnreadNotificationCount: () => request<{ unread_count: number }>('/notifications/unread-count'),
  markNotificationRead: (id: string) => request<void>(`/notifications/${id}/read`, { method: 'POST' }),
  markAllNotificationsRead: () => request<void>('/notifications/read-all', { method: 'POST' }),
  dismissNotification: (id: string) => request<void>(`/notifications/${id}`, { method: 'DELETE' }),
  clearAllNotifications: () => request<void>('/notifications', { method: 'DELETE' }),

  // Settings: Profile & Account
  getProfile: () => request<import('../types').User>('/settings/profile'),
  updateProfile: (data: { display_name: string; username: string; avatar?: string; theme?: string }) =>
    request<any>('/settings/profile', { method: 'PUT', body: JSON.stringify(data) }),
  changePassword: (data: { current_password: string; new_password: string }) =>
    request<any>('/settings/password', { method: 'PUT', body: JSON.stringify(data) }),

  // Settings: Notifications (ntfy, discord, signal)
  getNotificationConfigs: () => request<import('../types').NotificationConfig[]>('/settings/notifications'),
  saveNotificationConfig: (service: string, data: { enabled: boolean; config_json: string }) =>
    request<import('../types').NotificationConfig>(`/settings/notifications/${service}`, {
      method: 'PUT',
      body: JSON.stringify(data),
    }),
  testNotification: (service: string, config_json?: string) =>
    request<{ message: string }>(`/settings/notifications/${service}/test`, {
      method: 'POST',
      body: JSON.stringify({ config_json: config_json || '{}' }),
    }),

  // Settings: Scheduler
  getSchedulerConfig: () => request<import('../types').SchedulerConfig>('/settings/scheduler'),
  saveSchedulerConfig: (data: { enabled: boolean; interval_minutes: number }) =>
    request<import('../types').SchedulerConfig>('/settings/scheduler', {
      method: 'PUT',
      body: JSON.stringify(data),
    }),
  triggerSchedulerCheck: () => request<{ message: string }>('/settings/scheduler/run', { method: 'POST' }),

  // User Management
  listUsers: () => request<import('../types').ManagedUser[]>('/users'),
  createUser: (data: { username: string; password: string; display_name?: string; role: 'admin' | 'viewer' }) =>
    request<import('../types').ManagedUser>('/users', { method: 'POST', body: JSON.stringify(data) }),
  updateUser: (id: string, data: { display_name?: string; role?: 'admin' | 'viewer'; password?: string }) =>
    request<any>(`/users/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
  deleteUser: (id: string) => request<any>(`/users/${id}`, { method: 'DELETE' }),
};
