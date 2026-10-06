import React, { useEffect } from 'react';
import { X, CheckCheck, Bell, AlertTriangle, ArrowUpCircle, Info, AlertCircle, Clock } from 'lucide-react';
import { InAppNotification } from '../types';

interface NotificationsDrawerProps {
  isOpen: boolean;
  onClose: () => void;
  notifications: InAppNotification[];
  onDismiss: (id: string) => void;
  onDismissAll: () => void;
  onMarkAllRead: () => void;
}

function timeAgo(dateStr: string): string {
  try {
    const date = new Date(dateStr);
    const now = new Date();
    const seconds = Math.floor((now.getTime() - date.getTime()) / 1000);
    if (seconds < 60) return 'Just now';
    const minutes = Math.floor(seconds / 60);
    if (minutes < 60) return `${minutes}m ago`;
    const hours = Math.floor(minutes / 60);
    if (hours < 24) return `${hours}h ago`;
    const days = Math.floor(hours / 24);
    return `${days}d ago`;
  } catch {
    return dateStr;
  }
}

export const NotificationsDrawer: React.FC<NotificationsDrawerProps> = ({
  isOpen,
  onClose,
  notifications,
  onDismiss,
  onDismissAll,
  onMarkAllRead,
}) => {
  // Mark all notifications as read when opening drawer
  useEffect(() => {
    if (isOpen && notifications.some((n) => !n.read)) {
      onMarkAllRead();
    }
  }, [isOpen, notifications, onMarkAllRead]);

  // Close on Escape key
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && isOpen) {
        onClose();
      }
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [isOpen, onClose]);

  if (!isOpen) return null;

  return (
    <div className="fixed inset-0 z-50 flex justify-end">
      {/* Backdrop */}
      <div
        className="fixed inset-0 bg-slate-950/70 backdrop-blur-sm transition-opacity animate-in fade-in duration-200"
        onClick={onClose}
      />

      {/* Slide-out Drawer from Right */}
      <div className="relative w-full max-w-md bg-slate-900 border-l border-slate-800 shadow-2xl flex flex-col z-10 animate-in slide-in-from-right duration-250">
        {/* Header */}
        <div className="flex items-center justify-between px-5 py-4 border-b border-slate-800 bg-slate-900/90 backdrop-blur-sm">
          <div className="flex items-center gap-2.5">
            <div className="p-2 rounded-lg bg-sky-500/10 text-sky-400 border border-sky-500/20">
              <Bell className="w-4 h-4" />
            </div>
            <div>
              <h2 className="text-sm font-semibold text-slate-100 flex items-center gap-2">
                Notifications
                {notifications.length > 0 && (
                  <span className="text-[11px] font-mono px-2 py-0.5 rounded-full bg-slate-800 text-slate-400 border border-slate-700">
                    {notifications.length}
                  </span>
                )}
              </h2>
              <p className="text-[11px] text-slate-400">Fleet alerts and update events</p>
            </div>
          </div>

          <div className="flex items-center gap-2">
            {notifications.length > 0 && (
              <button
                onClick={onDismissAll}
                className="flex items-center gap-1.5 text-xs text-slate-400 hover:text-rose-400 hover:bg-slate-800/80 px-2.5 py-1.5 rounded-lg border border-transparent hover:border-slate-700 transition-colors"
                title="Dismiss All"
              >
                <CheckCheck className="w-3.5 h-3.5" />
                <span>Dismiss all</span>
              </button>
            )}
            <button
              onClick={onClose}
              className="p-1.5 text-slate-400 hover:text-slate-200 hover:bg-slate-800 rounded-lg transition-colors"
              title="Close"
            >
              <X className="w-4 h-4" />
            </button>
          </div>
        </div>

        {/* Notifications List */}
        <div className="flex-1 overflow-y-auto p-4 space-y-3">
          {notifications.length === 0 ? (
            <div className="h-full flex flex-col items-center justify-center text-center p-8 text-slate-500">
              <div className="w-12 h-12 rounded-2xl bg-slate-800/60 flex items-center justify-center mb-3 text-slate-600">
                <Bell className="w-6 h-6" />
              </div>
              <p className="text-sm font-medium text-slate-300">All caught up!</p>
              <p className="text-xs text-slate-500 mt-1 max-w-[220px]">
                No pending notifications or image updates across your fleet.
              </p>
            </div>
          ) : (
            notifications.map((n) => {
              const isUpdate = n.type === 'update';
              const isWarning = n.type === 'warning';
              const isError = n.type === 'error';

              const icon = isUpdate ? (
                <ArrowUpCircle className="w-4 h-4 text-emerald-400" />
              ) : isWarning ? (
                <AlertTriangle className="w-4 h-4 text-amber-400" />
              ) : isError ? (
                <AlertCircle className="w-4 h-4 text-rose-400" />
              ) : (
                <Info className="w-4 h-4 text-sky-400" />
              );

              const badgeColor = isUpdate
                ? 'bg-emerald-950/40 border-emerald-500/30 text-emerald-300'
                : isWarning
                ? 'bg-amber-950/40 border-amber-500/30 text-amber-300'
                : isError
                ? 'bg-rose-950/40 border-rose-500/30 text-rose-300'
                : 'bg-sky-950/40 border-sky-500/30 text-sky-300';

              return (
                <div
                  key={n.id}
                  className={`group relative p-3.5 rounded-xl border transition-all ${
                    n.read
                      ? 'bg-slate-900/60 border-slate-800/80 text-slate-300 hover:border-slate-700'
                      : 'bg-slate-800/70 border-sky-500/30 shadow-sm text-slate-100 hover:border-sky-500/50'
                  }`}
                >
                  <div className="flex items-start justify-between gap-3">
                    <div className="flex items-start gap-2.5 flex-1 min-w-0">
                      <div className={`p-1.5 rounded-lg border shrink-0 mt-0.5 ${badgeColor}`}>
                        {icon}
                      </div>
                      <div className="flex-1 min-w-0">
                        <div className="flex items-center gap-2 mb-1 flex-wrap">
                          <h3 className="text-xs font-semibold truncate text-slate-100">
                            {n.title}
                          </h3>
                          {!n.read && (
                            <span className="w-1.5 h-1.5 rounded-full bg-sky-400 shrink-0" />
                          )}
                        </div>
                        <p className="text-xs text-slate-400 leading-relaxed break-words whitespace-pre-wrap font-sans">
                          {n.message}
                        </p>
                        <div className="flex items-center gap-2 mt-2 text-[10px] text-slate-500 font-mono">
                          <Clock className="w-3 h-3 text-slate-600" />
                          <span>{timeAgo(n.created_at)}</span>
                          {n.host_id && (
                            <>
                              <span>&bull;</span>
                              <span className="px-1.5 py-0.2 rounded bg-slate-800 text-slate-400">
                                {n.host_id}
                              </span>
                            </>
                          )}
                        </div>
                      </div>
                    </div>

                    <button
                      onClick={() => onDismiss(n.id)}
                      className="text-slate-500 hover:text-rose-400 p-1 rounded-md hover:bg-slate-800/80 transition-colors opacity-70 group-hover:opacity-100"
                      title="Dismiss"
                    >
                      <X className="w-3.5 h-3.5" />
                    </button>
                  </div>
                </div>
              );
            })
          )}
        </div>
      </div>
    </div>
  );
};
