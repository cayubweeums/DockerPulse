import React, { useState } from 'react';
import {
  ArrowLeft,
  User,
  Bell,
  Users,
  Settings as SettingsIcon,
  Shield,
  Palette
} from 'lucide-react';
import { ProfileSettings } from './ProfileSettings';
import { NotificationSettings } from './NotificationSettings';
import { UserManagement } from './UserManagement';
import { User as UserType } from '../../types';

interface SettingsViewProps {
  onBackToDashboard: () => void;
  currentUser: UserType | null;
  onUserUpdated: (u: UserType) => void;
}

export const SettingsView: React.FC<SettingsViewProps> = ({
  onBackToDashboard,
  currentUser,
  onUserUpdated,
}) => {
  const [activeTab, setActiveTab] = useState<'profile' | 'notifications' | 'users'>('profile');

  const isAdmin = currentUser?.role === 'admin';

  return (
    <div className="min-h-screen bg-slate-950 text-slate-100 flex flex-col font-sans">
      {/* Top Header Bar */}
      <header className="border-b border-slate-800 bg-slate-900/80 backdrop-blur-md sticky top-0 z-20">
        <div className="max-w-7xl mx-auto px-4 h-14 flex items-center justify-between">
          <div className="flex items-center gap-3">
            <button
              onClick={onBackToDashboard}
              className="flex items-center gap-2 px-3 py-1.5 rounded-xl bg-slate-800/80 hover:bg-slate-700/80 text-slate-300 hover:text-white text-xs font-semibold border border-slate-700/60 transition-colors shadow-sm"
            >
              <ArrowLeft className="w-3.5 h-3.5" />
              <span>Dashboard</span>
            </button>
            <div className="h-4 w-px bg-slate-800" />
            <div className="flex items-center gap-2">
              <div className="p-1.5 rounded-lg bg-sky-500/10 text-sky-400 border border-sky-500/20">
                <SettingsIcon className="w-4 h-4" />
              </div>
              <h1 className="text-sm font-semibold text-slate-100">System Settings</h1>
            </div>
          </div>

          <div className="flex items-center gap-2 text-xs text-slate-400 font-mono">
            <span>{currentUser?.display_name || currentUser?.username}</span>
            <span className="text-[10px] uppercase font-bold px-2 py-0.5 rounded-full bg-slate-800 border border-slate-700 text-sky-400">
              {currentUser?.role}
            </span>
          </div>
        </div>
      </header>

      {/* Main Container with Left Sidebar */}
      <div className="flex-1 max-w-7xl w-full mx-auto px-4 py-8 flex flex-col md:flex-row gap-8">
        {/* Left Navigation Sidebar */}
        <aside className="w-full md:w-64 shrink-0">
          <div className="bg-slate-900/60 border border-slate-800 rounded-2xl p-3 shadow-sm sticky top-20">
            <div className="px-3 py-2 text-[11px] font-bold uppercase tracking-wider text-slate-500">
              Settings Menu
            </div>
            <nav className="space-y-1">
              <button
                onClick={() => setActiveTab('profile')}
                className={`w-full flex items-center gap-3 px-3.5 py-2.5 rounded-xl text-xs font-medium transition-all ${
                  activeTab === 'profile'
                    ? 'bg-sky-600 text-white shadow-sm shadow-sky-600/30'
                    : 'text-slate-400 hover:text-slate-200 hover:bg-slate-800/60'
                }`}
              >
                <User className="w-4 h-4 shrink-0" />
                <span className="flex-1 text-left">Profile & Account</span>
              </button>

              <button
                onClick={() => setActiveTab('notifications')}
                className={`w-full flex items-center gap-3 px-3.5 py-2.5 rounded-xl text-xs font-medium transition-all ${
                  activeTab === 'notifications'
                    ? 'bg-sky-600 text-white shadow-sm shadow-sky-600/30'
                    : 'text-slate-400 hover:text-slate-200 hover:bg-slate-800/60'
                }`}
              >
                <Bell className="w-4 h-4 shrink-0" />
                <span className="flex-1 text-left">Notifications & Updates</span>
              </button>

              {isAdmin && (
                <button
                  onClick={() => setActiveTab('users')}
                  className={`w-full flex items-center gap-3 px-3.5 py-2.5 rounded-xl text-xs font-medium transition-all ${
                    activeTab === 'users'
                      ? 'bg-sky-600 text-white shadow-sm shadow-sky-600/30'
                      : 'text-slate-400 hover:text-slate-200 hover:bg-slate-800/60'
                  }`}
                >
                  <Users className="w-4 h-4 shrink-0" />
                  <span className="flex-1 text-left">User Management</span>
                </button>
              )}
            </nav>

            <div className="mt-6 pt-4 border-t border-slate-800/80 px-3">
              <div className="flex items-center gap-2.5">
                {currentUser?.avatar ? (
                  <img
                    src={currentUser.avatar}
                    alt=""
                    className="w-8 h-8 rounded-lg object-cover border border-slate-700 shrink-0"
                  />
                ) : (
                  <div className="w-8 h-8 rounded-lg bg-gradient-to-br from-sky-600 to-indigo-600 flex items-center justify-center text-[11px] font-bold text-white shrink-0">
                    {(currentUser?.display_name || currentUser?.username || 'U').slice(0, 2).toUpperCase()}
                  </div>
                )}
                <div className="min-w-0 flex-1">
                  <div className="text-xs font-semibold text-slate-200 truncate">
                    {currentUser?.display_name || currentUser?.username}
                  </div>
                  <div className="text-[10px] text-slate-500 font-mono capitalize">
                    {currentUser?.role}
                  </div>
                </div>
              </div>
            </div>
          </div>
        </aside>

        {/* Content Area */}
        <main className="flex-1 min-w-0">
          {activeTab === 'profile' && (
            <ProfileSettings currentUser={currentUser} onUserUpdated={onUserUpdated} />
          )}

          {activeTab === 'notifications' && <NotificationSettings />}

          {activeTab === 'users' && <UserManagement currentUser={currentUser} />}
        </main>
      </div>
    </div>
  );
};
