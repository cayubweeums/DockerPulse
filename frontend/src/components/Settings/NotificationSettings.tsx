import React, { useState, useEffect } from 'react';
import {
  Bell,
  Check,
  AlertTriangle,
  Clock,
  RefreshCw,
  Send,
  ExternalLink,
  X,
  MessageSquare,
  ShieldCheck,
  Radio,
  Save,
  CheckCircle2
} from 'lucide-react';
import { api } from '../../api/client';
import { NotificationConfig, SchedulerConfig } from '../../types';

export const NotificationSettings: React.FC = () => {
  const [configs, setConfigs] = useState<Record<string, NotificationConfig>>({});
  const [scheduler, setScheduler] = useState<SchedulerConfig | null>(null);
  const [loading, setLoading] = useState(true);

  // Active service modal for configuration
  const [activeModal, setActiveModal] = useState<'ntfy' | 'discord' | 'signal' | null>(null);
  const [modalForm, setModalForm] = useState<any>({});
  const [modalEnabled, setModalEnabled] = useState(false);
  const [testing, setTesting] = useState(false);
  const [testResult, setTestResult] = useState<{ type: 'success' | 'error'; message: string } | null>(null);
  const [savingService, setSavingService] = useState(false);

  // Scheduler state
  const [savingScheduler, setSavingScheduler] = useState(false);
  const [triggeringCheck, setTriggeringCheck] = useState(false);
  const [schedulerMsg, setSchedulerMsg] = useState<{ type: 'success' | 'error'; message: string } | null>(null);

  useEffect(() => {
    loadData();
  }, []);

  const loadData = async () => {
    setLoading(true);
    try {
      const [notifList, sched] = await Promise.all([
        api.getNotificationConfigs(),
        api.getSchedulerConfig(),
      ]);

      const map: Record<string, NotificationConfig> = {};
      notifList.forEach((c) => {
        map[c.id] = c;
      });
      setConfigs(map);
      setScheduler(sched);
    } catch (err) {
      console.error('Failed to load notification settings:', err);
    } finally {
      setLoading(false);
    }
  };

  const openConfigModal = (service: 'ntfy' | 'discord' | 'signal') => {
    setActiveModal(service);
    setTestResult(null);
    const existing = configs[service];
    let parsed: any = {};
    if (existing?.config_json) {
      try {
        parsed = JSON.parse(existing.config_json);
      } catch {}
    }

    if (service === 'ntfy') {
      setModalForm({
        server_url: parsed.server_url || 'https://ntfy.sh',
        topic: parsed.topic || '',
        token: parsed.token || '',
        priority: parsed.priority || 'default',
        tags: parsed.tags || 'docker,package',
      });
    } else if (service === 'discord') {
      setModalForm({
        webhook_url: parsed.webhook_url || '',
        username: parsed.username || 'DockerPulse',
        avatar_url: parsed.avatar_url || '',
      });
    } else if (service === 'signal') {
      setModalForm({
        endpoint_url: parsed.endpoint_url || 'http://signal-cli:8080/v2/send',
        number: parsed.number || '',
        recipients: parsed.recipients || '',
      });
    }
    setModalEnabled(existing ? existing.enabled : false);
  };

  const handleTestService = async () => {
    if (!activeModal) return;
    setTesting(true);
    setTestResult(null);
    try {
      const jsonStr = JSON.stringify(modalForm);
      const res = await api.testNotification(activeModal, jsonStr);
      setTestResult({ type: 'success', message: res.message || 'Test notification sent successfully!' });

      // Refresh configs to update status indicator
      const list = await api.getNotificationConfigs();
      const map: Record<string, NotificationConfig> = {};
      list.forEach((c) => {
        map[c.id] = c;
      });
      setConfigs(map);
    } catch (err: any) {
      setTestResult({ type: 'error', message: err.message || 'Failed to send test alert' });
    } finally {
      setTesting(false);
    }
  };

  const handleSaveServiceConfig = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!activeModal) return;
    setSavingService(true);
    try {
      const jsonStr = JSON.stringify(modalForm);
      const updated = await api.saveNotificationConfig(activeModal, {
        enabled: modalEnabled,
        config_json: jsonStr,
      });

      setConfigs((prev) => ({ ...prev, [activeModal]: updated }));
      setActiveModal(null);
    } catch (err: any) {
      setTestResult({ type: 'error', message: err.message || 'Failed to save configuration' });
    } finally {
      setSavingService(false);
    }
  };

  const handleSaveScheduler = async (enabled: boolean, intervalMinutes: number) => {
    if (!scheduler) return;
    setSavingScheduler(true);
    setSchedulerMsg(null);
    try {
      const updated = await api.saveSchedulerConfig({
        enabled,
        interval_minutes: intervalMinutes,
      });
      setScheduler(updated);
      setSchedulerMsg({ type: 'success', message: 'Update schedule saved!' });
    } catch (err: any) {
      setSchedulerMsg({ type: 'error', message: err.message || 'Failed to save schedule' });
    } finally {
      setSavingScheduler(false);
    }
  };

  const handleTriggerNow = async () => {
    setTriggeringCheck(true);
    setSchedulerMsg(null);
    try {
      await api.triggerSchedulerCheck();
      setSchedulerMsg({
        type: 'success',
        message: 'Update check triggered! Running across all fleet hosts in background...',
      });
    } catch (err: any) {
      setSchedulerMsg({ type: 'error', message: err.message || 'Failed to trigger update check' });
    } finally {
      setTriggeringCheck(false);
    }
  };

  const renderStatusCircle = (cfg?: NotificationConfig) => {
    if (!cfg || !cfg.enabled || cfg.status === 'unconfigured') {
      return (
        <span
          className="w-4 h-4 rounded-full border-2 border-slate-600 bg-transparent shrink-0"
          title="Not configured"
        />
      );
    }
    if (cfg.status === 'error') {
      return (
        <span
          className="w-4 h-4 rounded-full bg-amber-500/20 text-amber-400 border border-amber-500/50 flex items-center justify-center shrink-0 shadow-sm shadow-amber-500/20"
          title={cfg.last_error || 'Configuration error'}
        >
          <AlertTriangle className="w-2.5 h-2.5" />
        </span>
      );
    }
    return (
      <span
        className="w-4 h-4 rounded-full bg-emerald-500/20 text-emerald-400 border border-emerald-500/50 flex items-center justify-center shrink-0 shadow-sm shadow-emerald-500/20"
        title="Enabled & verified"
      >
        <Check className="w-2.5 h-2.5" />
      </span>
    );
  };

  return (
    <div className="space-y-8 max-w-4xl">
      {/* Top Section: 3 External Notification Services */}
      <div className="bg-slate-900/60 border border-slate-800 rounded-2xl p-6 shadow-sm">
        <div className="flex items-center justify-between mb-1">
          <h3 className="text-base font-semibold text-slate-100 flex items-center gap-2">
            <Bell className="w-4 h-4 text-sky-400" />
            External Notification Channels
          </h3>
          <div className="flex items-center gap-3 text-[11px] text-slate-500 font-mono">
            <span className="flex items-center gap-1.5">
              <span className="w-2.5 h-2.5 rounded-full border border-slate-600" /> Unconfigured
            </span>
            <span className="flex items-center gap-1.5">
              <span className="w-2.5 h-2.5 rounded-full bg-emerald-400" /> Enabled
            </span>
            <span className="flex items-center gap-1.5">
              <span className="w-2.5 h-2.5 rounded-full bg-amber-400" /> Error
            </span>
          </div>
        </div>
        <p className="text-xs text-slate-400 mb-6">
          Dispatch instant alerts when new container image updates are discovered across your servers. You can enable multiple services at once.
        </p>

        <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
          {/* ntfy Card */}
          <button
            onClick={() => openConfigModal('ntfy')}
            className={`group text-left p-4 rounded-xl border transition-all flex flex-col justify-between ${
              configs['ntfy']?.enabled
                ? 'bg-slate-900/90 border-slate-700 hover:border-sky-500 shadow-sm'
                : 'bg-slate-950/60 border-slate-800/80 hover:border-slate-700'
            }`}
          >
            <div>
              <div className="flex items-center justify-between mb-3">
                <div className="w-10 h-10 rounded-xl bg-sky-500/10 border border-sky-500/20 flex items-center justify-center text-sky-400 group-hover:scale-105 transition-transform">
                  <Radio className="w-5 h-5" />
                </div>
                {renderStatusCircle(configs['ntfy'])}
              </div>
              <h4 className="text-sm font-semibold text-slate-100 flex items-center gap-1.5">
                ntfy
              </h4>
              <p className="text-xs text-slate-400 mt-1">
                Push notifications via ntfy.sh or self-hosted HTTP pub-sub server.
              </p>
            </div>
            <div className="mt-4 pt-3 border-t border-slate-800/80 flex items-center justify-between text-[11px] text-slate-400 font-medium">
              <span>{configs['ntfy']?.enabled ? 'Configured' : 'Set up'}</span>
              <span className="text-sky-400 group-hover:translate-x-0.5 transition-transform">&rarr;</span>
            </div>
          </button>

          {/* Discord Card */}
          <button
            onClick={() => openConfigModal('discord')}
            className={`group text-left p-4 rounded-xl border transition-all flex flex-col justify-between ${
              configs['discord']?.enabled
                ? 'bg-slate-900/90 border-slate-700 hover:border-indigo-500 shadow-sm'
                : 'bg-slate-950/60 border-slate-800/80 hover:border-slate-700'
            }`}
          >
            <div>
              <div className="flex items-center justify-between mb-3">
                <div className="w-10 h-10 rounded-xl bg-indigo-500/10 border border-indigo-500/20 flex items-center justify-center text-indigo-400 group-hover:scale-105 transition-transform">
                  <MessageSquare className="w-5 h-5" />
                </div>
                {renderStatusCircle(configs['discord'])}
              </div>
              <h4 className="text-sm font-semibold text-slate-100 flex items-center gap-1.5">
                Discord
              </h4>
              <p className="text-xs text-slate-400 mt-1">
                Post formatted embeds directly into your Discord channel via webhook.
              </p>
            </div>
            <div className="mt-4 pt-3 border-t border-slate-800/80 flex items-center justify-between text-[11px] text-slate-400 font-medium">
              <span>{configs['discord']?.enabled ? 'Configured' : 'Set up'}</span>
              <span className="text-indigo-400 group-hover:translate-x-0.5 transition-transform">&rarr;</span>
            </div>
          </button>

          {/* Signal Card */}
          <button
            onClick={() => openConfigModal('signal')}
            className={`group text-left p-4 rounded-xl border transition-all flex flex-col justify-between ${
              configs['signal']?.enabled
                ? 'bg-slate-900/90 border-slate-700 hover:border-emerald-500 shadow-sm'
                : 'bg-slate-950/60 border-slate-800/80 hover:border-slate-700'
            }`}
          >
            <div>
              <div className="flex items-center justify-between mb-3">
                <div className="w-10 h-10 rounded-xl bg-emerald-500/10 border border-emerald-500/20 flex items-center justify-center text-emerald-400 group-hover:scale-105 transition-transform">
                  <ShieldCheck className="w-5 h-5" />
                </div>
                {renderStatusCircle(configs['signal'])}
              </div>
              <h4 className="text-sm font-semibold text-slate-100 flex items-center gap-1.5">
                Signal
              </h4>
              <p className="text-xs text-slate-400 mt-1">
                End-to-end encrypted private messages or groups using signal-cli.
              </p>
            </div>
            <div className="mt-4 pt-3 border-t border-slate-800/80 flex items-center justify-between text-[11px] text-slate-400 font-medium">
              <span>{configs['signal']?.enabled ? 'Configured' : 'Set up'}</span>
              <span className="text-emerald-400 group-hover:translate-x-0.5 transition-transform">&rarr;</span>
            </div>
          </button>
        </div>
      </div>

      {/* Bottom Section: Check for Update Frequency Schedule */}
      <div className="bg-slate-900/60 border border-slate-800 rounded-2xl p-6 shadow-sm">
        <h3 className="text-base font-semibold text-slate-100 mb-1 flex items-center gap-2">
          <Clock className="w-4 h-4 text-emerald-400" />
          Fleet Update Check Schedule
        </h3>
        <p className="text-xs text-slate-400 mb-6">
          Automatically query container registries in the background to scan for newer image tags across all connected Docker hosts.
        </p>

        {schedulerMsg && (
          <div
            className={`mb-6 p-3.5 rounded-xl text-xs flex items-center gap-2.5 border ${
              schedulerMsg.type === 'success'
                ? 'bg-emerald-950/40 border-emerald-500/30 text-emerald-300'
                : 'bg-rose-950/40 border-rose-500/30 text-rose-300'
            }`}
          >
            {schedulerMsg.type === 'success' ? (
              <Check className="w-4 h-4 shrink-0 text-emerald-400" />
            ) : (
              <AlertTriangle className="w-4 h-4 shrink-0 text-rose-400" />
            )}
            <span>{schedulerMsg.message}</span>
          </div>
        )}

        {scheduler && (
          <div className="space-y-6">
            <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4 p-4 rounded-xl bg-slate-950/60 border border-slate-800/80">
              <div>
                <label className="text-sm font-medium text-slate-200 block">
                  Periodic Update Scanning
                </label>
                <p className="text-xs text-slate-400 mt-0.5">
                  When enabled, DockerPulse will periodically scan for container image updates without manual intervention.
                </p>
              </div>

              <label className="relative inline-flex items-center cursor-pointer shrink-0">
                <input
                  type="checkbox"
                  checked={scheduler.enabled}
                  onChange={(e) => handleSaveScheduler(e.target.checked, scheduler.interval_minutes)}
                  className="sr-only peer"
                />
                <div className="w-11 h-6 bg-slate-800 peer-focus:outline-none rounded-full peer peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:border-slate-300 after:border after:rounded-full after:h-5 after:w-5 after:transition-all peer-checked:bg-emerald-500" />
              </label>
            </div>

            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
              <div>
                <label className="text-xs font-medium text-slate-300 block mb-1.5">
                  Scan Frequency Interval
                </label>
                <select
                  value={scheduler.interval_minutes}
                  onChange={(e) => handleSaveScheduler(scheduler.enabled, Number(e.target.value))}
                  disabled={!scheduler.enabled || savingScheduler}
                  className="w-full bg-slate-950 border border-slate-800 focus:border-emerald-500 rounded-xl px-3.5 py-2 text-sm text-slate-100 outline-none transition-colors disabled:opacity-50"
                >
                  <option value={60}>Every 1 Hour</option>
                  <option value={180}>Every 3 Hours</option>
                  <option value={360}>Every 6 Hours (Recommended)</option>
                  <option value={720}>Every 12 Hours</option>
                  <option value={1440}>Every 24 Hours (Daily)</option>
                </select>
              </div>

              <div className="flex flex-col justify-end">
                <button
                  type="button"
                  onClick={handleTriggerNow}
                  disabled={triggeringCheck}
                  className="flex items-center justify-center gap-2 px-4 py-2 rounded-xl bg-slate-800 hover:bg-slate-700 text-xs font-semibold text-sky-400 border border-slate-700 transition-colors disabled:opacity-50"
                >
                  <RefreshCw className={`w-3.5 h-3.5 ${triggeringCheck ? 'animate-spin' : ''}`} />
                  <span>{triggeringCheck ? 'Running scan...' : 'Check For Updates Now'}</span>
                </button>
              </div>
            </div>

            <div className="pt-2 border-t border-slate-800/80 flex flex-wrap items-center justify-between text-[11px] text-slate-500 font-mono">
              <span>Last checked: {scheduler.last_run ? new Date(scheduler.last_run).toLocaleString() : 'Never'}</span>
              <span>Next check: {scheduler.next_run ? new Date(scheduler.next_run).toLocaleString() : 'Scheduled by interval'}</span>
            </div>
          </div>
        )}
      </div>

      {/* Service Configuration Modal */}
      {activeModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-slate-950/80 backdrop-blur-sm animate-in fade-in duration-200">
          <div className="bg-slate-900 border border-slate-800 rounded-2xl w-full max-w-lg shadow-2xl overflow-hidden flex flex-col animate-in zoom-in-95 duration-200">
            {/* Modal Header */}
            <div className="flex items-center justify-between px-6 py-4 border-b border-slate-800">
              <div className="flex items-center gap-3">
                <div className="p-2 rounded-xl bg-slate-800 border border-slate-700">
                  {activeModal === 'ntfy' && <Radio className="w-5 h-5 text-sky-400" />}
                  {activeModal === 'discord' && <MessageSquare className="w-5 h-5 text-indigo-400" />}
                  {activeModal === 'signal' && <ShieldCheck className="w-5 h-5 text-emerald-400" />}
                </div>
                <div>
                  <h3 className="text-sm font-semibold text-slate-100 uppercase tracking-wider">
                    {activeModal} Configuration
                  </h3>
                  <p className="text-xs text-slate-400">
                    Set up connection credentials and notification target
                  </p>
                </div>
              </div>
              <button
                onClick={() => setActiveModal(null)}
                className="p-1.5 text-slate-400 hover:text-slate-200 hover:bg-slate-800 rounded-lg transition-colors"
              >
                <X className="w-4 h-4" />
              </button>
            </div>

            {/* Modal Form */}
            <form onSubmit={handleSaveServiceConfig} className="p-6 space-y-4">
              {testResult && (
                <div
                  className={`p-3.5 rounded-xl text-xs flex items-center gap-2.5 border ${
                    testResult.type === 'success'
                      ? 'bg-emerald-950/40 border-emerald-500/30 text-emerald-300'
                      : 'bg-rose-950/40 border-rose-500/30 text-rose-300'
                  }`}
                >
                  {testResult.type === 'success' ? (
                    <CheckCircle2 className="w-4 h-4 shrink-0 text-emerald-400" />
                  ) : (
                    <AlertTriangle className="w-4 h-4 shrink-0 text-rose-400" />
                  )}
                  <span className="flex-1">{testResult.message}</span>
                </div>
              )}

              {/* Enabled toggle */}
              <div className="flex items-center justify-between p-3 rounded-xl bg-slate-950/60 border border-slate-800">
                <div>
                  <span className="text-xs font-medium text-slate-200 block">
                    Enable this notification channel
                  </span>
                  <span className="text-[11px] text-slate-500">
                    Send fleet update alerts to this service
                  </span>
                </div>
                <label className="relative inline-flex items-center cursor-pointer">
                  <input
                    type="checkbox"
                    checked={modalEnabled}
                    onChange={(e) => setModalEnabled(e.target.checked)}
                    className="sr-only peer"
                  />
                  <div className="w-11 h-6 bg-slate-800 peer-focus:outline-none rounded-full peer peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:border-slate-300 after:border after:rounded-full after:h-5 after:w-5 after:transition-all peer-checked:bg-sky-500" />
                </label>
              </div>

              {/* Service specific fields */}
              {activeModal === 'ntfy' && (
                <>
                  <div>
                    <label className="text-xs font-medium text-slate-300 block mb-1">
                      Server URL
                    </label>
                    <input
                      type="url"
                      value={modalForm.server_url || ''}
                      onChange={(e) => setModalForm({ ...modalForm, server_url: e.target.value })}
                      placeholder="https://ntfy.sh"
                      className="w-full bg-slate-950 border border-slate-800 focus:border-sky-500 rounded-xl px-3 py-2 text-sm text-slate-100 placeholder-slate-600 outline-none"
                    />
                    <p className="text-[11px] text-slate-500 mt-1">Default: https://ntfy.sh</p>
                  </div>

                  <div>
                    <label className="text-xs font-medium text-slate-300 block mb-1">
                      Topic <span className="text-rose-400">*</span>
                    </label>
                    <input
                      type="text"
                      value={modalForm.topic || ''}
                      onChange={(e) => setModalForm({ ...modalForm, topic: e.target.value })}
                      required
                      placeholder="e.g. dockerpulse_alerts_9841"
                      className="w-full bg-slate-950 border border-slate-800 focus:border-sky-500 rounded-xl px-3 py-2 text-sm text-slate-100 placeholder-slate-600 outline-none"
                    />
                  </div>

                  <div>
                    <label className="text-xs font-medium text-slate-300 block mb-1">
                      Access Token (Optional)
                    </label>
                    <input
                      type="password"
                      value={modalForm.token || ''}
                      onChange={(e) => setModalForm({ ...modalForm, token: e.target.value })}
                      placeholder="Bearer token for protected topics"
                      className="w-full bg-slate-950 border border-slate-800 focus:border-sky-500 rounded-xl px-3 py-2 text-sm text-slate-100 placeholder-slate-600 outline-none"
                    />
                  </div>
                </>
              )}

              {activeModal === 'discord' && (
                <>
                  <div>
                    <label className="text-xs font-medium text-slate-300 block mb-1">
                      Discord Webhook URL <span className="text-rose-400">*</span>
                    </label>
                    <input
                      type="url"
                      value={modalForm.webhook_url || ''}
                      onChange={(e) => setModalForm({ ...modalForm, webhook_url: e.target.value })}
                      required
                      placeholder="https://discord.com/api/webhooks/..."
                      className="w-full bg-slate-950 border border-slate-800 focus:border-indigo-500 rounded-xl px-3 py-2 text-sm text-slate-100 placeholder-slate-600 outline-none"
                    />
                  </div>

                  <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                    <div>
                      <label className="text-xs font-medium text-slate-300 block mb-1">
                        Bot Username
                      </label>
                      <input
                        type="text"
                        value={modalForm.username || ''}
                        onChange={(e) => setModalForm({ ...modalForm, username: e.target.value })}
                        placeholder="DockerPulse"
                        className="w-full bg-slate-950 border border-slate-800 focus:border-indigo-500 rounded-xl px-3 py-2 text-sm text-slate-100 placeholder-slate-600 outline-none"
                      />
                    </div>
                    <div>
                      <label className="text-xs font-medium text-slate-300 block mb-1">
                        Avatar URL (Optional)
                      </label>
                      <input
                        type="url"
                        value={modalForm.avatar_url || ''}
                        onChange={(e) => setModalForm({ ...modalForm, avatar_url: e.target.value })}
                        placeholder="https://..."
                        className="w-full bg-slate-950 border border-slate-800 focus:border-indigo-500 rounded-xl px-3 py-2 text-sm text-slate-100 placeholder-slate-600 outline-none"
                      />
                    </div>
                  </div>
                </>
              )}

              {activeModal === 'signal' && (
                <>
                  <div>
                    <label className="text-xs font-medium text-slate-300 block mb-1">
                      Signal-CLI REST Endpoint URL <span className="text-rose-400">*</span>
                    </label>
                    <input
                      type="url"
                      value={modalForm.endpoint_url || ''}
                      onChange={(e) => setModalForm({ ...modalForm, endpoint_url: e.target.value })}
                      required
                      placeholder="http://signal-cli:8080/v2/send"
                      className="w-full bg-slate-950 border border-slate-800 focus:border-emerald-500 rounded-xl px-3 py-2 text-sm text-slate-100 placeholder-slate-600 outline-none"
                    />
                  </div>

                  <div>
                    <label className="text-xs font-medium text-slate-300 block mb-1">
                      Recipient Numbers / Group IDs <span className="text-rose-400">*</span>
                    </label>
                    <input
                      type="text"
                      value={modalForm.recipients || ''}
                      onChange={(e) => setModalForm({ ...modalForm, recipients: e.target.value })}
                      required
                      placeholder="+1234567890, +0987654321"
                      className="w-full bg-slate-950 border border-slate-800 focus:border-emerald-500 rounded-xl px-3 py-2 text-sm text-slate-100 placeholder-slate-600 outline-none"
                    />
                    <p className="text-[11px] text-slate-500 mt-1">Comma-separated international phone numbers or group IDs.</p>
                  </div>

                  <div>
                    <label className="text-xs font-medium text-slate-300 block mb-1">
                      Sender Number (Optional)
                    </label>
                    <input
                      type="text"
                      value={modalForm.number || ''}
                      onChange={(e) => setModalForm({ ...modalForm, number: e.target.value })}
                      placeholder="Registered signal-cli number"
                      className="w-full bg-slate-950 border border-slate-800 focus:border-emerald-500 rounded-xl px-3 py-2 text-sm text-slate-100 placeholder-slate-600 outline-none"
                    />
                  </div>
                </>
              )}

              <div className="flex items-center justify-between pt-4 border-t border-slate-800">
                <button
                  type="button"
                  onClick={handleTestService}
                  disabled={testing}
                  className="flex items-center gap-1.5 px-3 py-2 rounded-xl bg-slate-800 hover:bg-slate-700 text-slate-200 text-xs font-medium border border-slate-700 transition-colors disabled:opacity-50"
                >
                  <Send className={`w-3.5 h-3.5 ${testing ? 'animate-spin' : ''}`} />
                  <span>{testing ? 'Sending...' : 'Send Test Notification'}</span>
                </button>

                <div className="flex items-center gap-2">
                  <button
                    type="button"
                    onClick={() => setActiveModal(null)}
                    className="px-3 py-2 rounded-xl text-slate-400 hover:text-slate-200 hover:bg-slate-800 text-xs transition-colors"
                  >
                    Cancel
                  </button>
                  <button
                    type="submit"
                    disabled={savingService}
                    className="flex items-center gap-1.5 px-4 py-2 rounded-xl bg-sky-600 hover:bg-sky-500 text-white text-xs font-semibold shadow-sm transition-all disabled:opacity-50"
                  >
                    <Save className="w-3.5 h-3.5" />
                    <span>{savingService ? 'Saving...' : 'Save Configuration'}</span>
                  </button>
                </div>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  );
};
