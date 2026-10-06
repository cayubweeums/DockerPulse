import React, { useState } from 'react';
import { User, Lock, Upload, Check, AlertCircle, Palette, Save } from 'lucide-react';
import { api } from '../../api/client';
import { User as UserType } from '../../types';

interface ProfileSettingsProps {
  currentUser: UserType | null;
  onUserUpdated: (u: UserType) => void;
}

export const ProfileSettings: React.FC<ProfileSettingsProps> = ({ currentUser, onUserUpdated }) => {
  const [displayName, setDisplayName] = useState(currentUser?.display_name || currentUser?.username || '');
  const [username, setUsername] = useState(currentUser?.username || '');
  const [avatar, setAvatar] = useState(currentUser?.avatar || '');
  const [theme, setTheme] = useState(currentUser?.theme || 'dark');

  const [savingProfile, setSavingProfile] = useState(false);
  const [profileMsg, setProfileMsg] = useState<{ type: 'success' | 'error'; text: string } | null>(null);

  // Password change state
  const [currentPassword, setCurrentPassword] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [savingPassword, setSavingPassword] = useState(false);
  const [passwordMsg, setPasswordMsg] = useState<{ type: 'success' | 'error'; text: string } | null>(null);

  const handleAvatarFileUpload = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;

    if (file.size > 2 * 1024 * 1024) {
      setProfileMsg({ type: 'error', text: 'Image file size must be under 2MB' });
      return;
    }

    const reader = new FileReader();
    reader.onload = (uploadEvent) => {
      const base64 = uploadEvent.target?.result as string;
      setAvatar(base64);
    };
    reader.readAsDataURL(file);
  };

  const handleSaveProfile = async (e: React.FormEvent) => {
    e.preventDefault();
    setSavingProfile(true);
    setProfileMsg(null);

    try {
      const res = await api.updateProfile({
        display_name: displayName,
        username,
        avatar,
        theme,
      });

      if (currentUser) {
        onUserUpdated({
          ...currentUser,
          display_name: displayName,
          username,
          avatar,
          theme,
        });
      }
      setProfileMsg({ type: 'success', text: res.message || 'Profile updated successfully!' });
    } catch (err: any) {
      setProfileMsg({ type: 'error', text: err.message || 'Failed to update profile' });
    } finally {
      setSavingProfile(false);
    }
  };

  const handleChangePassword = async (e: React.FormEvent) => {
    e.preventDefault();
    setPasswordMsg(null);

    if (newPassword !== confirmPassword) {
      setPasswordMsg({ type: 'error', text: 'New passwords do not match' });
      return;
    }
    if (newPassword.length < 6) {
      setPasswordMsg({ type: 'error', text: 'Password must be at least 6 characters' });
      return;
    }

    setSavingPassword(true);
    try {
      await api.changePassword({
        current_password: currentPassword,
        new_password: newPassword,
      });
      setPasswordMsg({ type: 'success', text: 'Password changed successfully!' });
      setCurrentPassword('');
      setNewPassword('');
      setConfirmPassword('');
    } catch (err: any) {
      setPasswordMsg({ type: 'error', text: err.message || 'Failed to change password' });
    } finally {
      setSavingPassword(false);
    }
  };

  const initials = (displayName || username || 'U')
    .slice(0, 2)
    .toUpperCase();

  return (
    <div className="space-y-8 max-w-4xl">
      {/* Profile Details Section */}
      <div className="bg-slate-900/60 border border-slate-800 rounded-2xl p-6 shadow-sm">
        <h3 className="text-base font-semibold text-slate-100 mb-1 flex items-center gap-2">
          <User className="w-4 h-4 text-sky-400" />
          Profile Information
        </h3>
        <p className="text-xs text-slate-400 mb-6">
          Update your public profile, username, and customize your account avatar.
        </p>

        {profileMsg && (
          <div
            className={`mb-6 p-3.5 rounded-xl text-xs flex items-center gap-2.5 border ${
              profileMsg.type === 'success'
                ? 'bg-emerald-950/40 border-emerald-500/30 text-emerald-300'
                : 'bg-rose-950/40 border-rose-500/30 text-rose-300'
            }`}
          >
            {profileMsg.type === 'success' ? (
              <Check className="w-4 h-4 shrink-0 text-emerald-400" />
            ) : (
              <AlertCircle className="w-4 h-4 shrink-0 text-rose-400" />
            )}
            <span>{profileMsg.text}</span>
          </div>
        )}

        <form onSubmit={handleSaveProfile} className="space-y-6">
          {/* Avatar row */}
          <div className="flex flex-col sm:flex-row items-start sm:items-center gap-5 pb-6 border-b border-slate-800/80">
            <div className="relative group shrink-0">
              {avatar ? (
                <img
                  src={avatar}
                  alt="Avatar"
                  className="w-20 h-20 rounded-2xl object-cover border-2 border-slate-700 bg-slate-800"
                />
              ) : (
                <div className="w-20 h-20 rounded-2xl bg-gradient-to-br from-sky-600 to-indigo-700 flex items-center justify-center text-white text-xl font-bold border-2 border-slate-700 shadow-inner">
                  {initials}
                </div>
              )}
            </div>

            <div className="space-y-2 flex-1">
              <label className="text-xs font-medium text-slate-300 block">Profile Picture</label>
              <div className="flex flex-wrap items-center gap-2.5">
                <label className="cursor-pointer flex items-center gap-2 px-3 py-1.5 rounded-xl bg-slate-800 hover:bg-slate-700 text-xs font-medium text-slate-200 border border-slate-700 transition-colors">
                  <Upload className="w-3.5 h-3.5 text-sky-400" />
                  <span>Upload Image</span>
                  <input
                    type="file"
                    accept="image/*"
                    onChange={handleAvatarFileUpload}
                    className="hidden"
                  />
                </label>
                {avatar && (
                  <button
                    type="button"
                    onClick={() => setAvatar('')}
                    className="px-3 py-1.5 rounded-xl bg-slate-800/60 hover:bg-rose-950/30 text-xs text-slate-400 hover:text-rose-400 border border-slate-700 hover:border-rose-500/30 transition-colors"
                  >
                    Remove
                  </button>
                )}
              </div>
              <p className="text-[11px] text-slate-500">
                Recommended: square PNG or JPG under 2MB. Defaults to your user initials if none uploaded.
              </p>
            </div>
          </div>

          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
            <div>
              <label className="text-xs font-medium text-slate-300 block mb-1.5">
                Display Name
              </label>
              <input
                type="text"
                value={displayName}
                onChange={(e) => setDisplayName(e.target.value)}
                placeholder="e.g. Alex Mason"
                className="w-full bg-slate-950 border border-slate-800 focus:border-sky-500 rounded-xl px-3.5 py-2 text-sm text-slate-100 placeholder-slate-600 outline-none transition-colors"
              />
            </div>

            <div>
              <label className="text-xs font-medium text-slate-300 block mb-1.5">
                Username
              </label>
              <input
                type="text"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                required
                className="w-full bg-slate-950 border border-slate-800 focus:border-sky-500 rounded-xl px-3.5 py-2 text-sm text-slate-100 placeholder-slate-600 outline-none transition-colors"
              />
            </div>
          </div>

          <div className="flex justify-end pt-2">
            <button
              type="submit"
              disabled={savingProfile}
              className="flex items-center gap-2 px-4 py-2 rounded-xl bg-sky-600 hover:bg-sky-500 text-white text-xs font-semibold shadow-sm shadow-sky-600/30 disabled:opacity-50 transition-all"
            >
              <Save className="w-3.5 h-3.5" />
              <span>{savingProfile ? 'Saving...' : 'Save Changes'}</span>
            </button>
          </div>
        </form>
      </div>

      {/* Password Management Section */}
      <div className="bg-slate-900/60 border border-slate-800 rounded-2xl p-6 shadow-sm">
        <h3 className="text-base font-semibold text-slate-100 mb-1 flex items-center gap-2">
          <Lock className="w-4 h-4 text-amber-400" />
          Change Password
        </h3>
        <p className="text-xs text-slate-400 mb-6">
          Ensure your account uses a secure password of at least 6 characters.
        </p>

        {passwordMsg && (
          <div
            className={`mb-6 p-3.5 rounded-xl text-xs flex items-center gap-2.5 border ${
              passwordMsg.type === 'success'
                ? 'bg-emerald-950/40 border-emerald-500/30 text-emerald-300'
                : 'bg-rose-950/40 border-rose-500/30 text-rose-300'
            }`}
          >
            {passwordMsg.type === 'success' ? (
              <Check className="w-4 h-4 shrink-0 text-emerald-400" />
            ) : (
              <AlertCircle className="w-4 h-4 shrink-0 text-rose-400" />
            )}
            <span>{passwordMsg.text}</span>
          </div>
        )}

        <form onSubmit={handleChangePassword} className="space-y-4">
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
            <div>
              <label className="text-xs font-medium text-slate-300 block mb-1.5">
                Current Password
              </label>
              <input
                type="password"
                value={currentPassword}
                onChange={(e) => setCurrentPassword(e.target.value)}
                required
                className="w-full bg-slate-950 border border-slate-800 focus:border-amber-500 rounded-xl px-3.5 py-2 text-sm text-slate-100 placeholder-slate-600 outline-none transition-colors"
              />
            </div>

            <div>
              <label className="text-xs font-medium text-slate-300 block mb-1.5">
                New Password
              </label>
              <input
                type="password"
                value={newPassword}
                onChange={(e) => setNewPassword(e.target.value)}
                required
                minLength={6}
                className="w-full bg-slate-950 border border-slate-800 focus:border-amber-500 rounded-xl px-3.5 py-2 text-sm text-slate-100 placeholder-slate-600 outline-none transition-colors"
              />
            </div>

            <div>
              <label className="text-xs font-medium text-slate-300 block mb-1.5">
                Confirm New Password
              </label>
              <input
                type="password"
                value={confirmPassword}
                onChange={(e) => setConfirmPassword(e.target.value)}
                required
                minLength={6}
                className="w-full bg-slate-950 border border-slate-800 focus:border-amber-500 rounded-xl px-3.5 py-2 text-sm text-slate-100 placeholder-slate-600 outline-none transition-colors"
              />
            </div>
          </div>

          <div className="flex justify-end pt-2">
            <button
              type="submit"
              disabled={savingPassword}
              className="flex items-center gap-2 px-4 py-2 rounded-xl bg-slate-800 hover:bg-slate-700 text-slate-100 text-xs font-semibold border border-slate-700 disabled:opacity-50 transition-colors"
            >
              <Lock className="w-3.5 h-3.5 text-amber-400" />
              <span>{savingPassword ? 'Updating...' : 'Update Password'}</span>
            </button>
          </div>
        </form>
      </div>

      {/* Theme Preference Section */}
      <div className="bg-slate-900/60 border border-slate-800 rounded-2xl p-6 shadow-sm">
        <h3 className="text-base font-semibold text-slate-100 mb-1 flex items-center gap-2">
          <Palette className="w-4 h-4 text-purple-400" />
          Appearance & Theme
        </h3>
        <p className="text-xs text-slate-400 mb-6">
          Customize the visual interface of DockerPulse. More custom themes will be added in future updates.
        </p>

        <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
          {/* Default Dark Theme */}
          <div
            onClick={() => setTheme('dark')}
            className={`cursor-pointer p-4 rounded-xl border transition-all ${
              theme === 'dark'
                ? 'border-sky-500 bg-sky-950/20 ring-1 ring-sky-500'
                : 'border-slate-800 bg-slate-950/60 hover:border-slate-700'
            }`}
          >
            <div className="flex items-center justify-between mb-3">
              <div className="flex items-center gap-2">
                <div className="w-3.5 h-3.5 rounded-full bg-slate-900 border border-slate-700" />
                <span className="text-xs font-semibold text-slate-100">Midnight Dark</span>
              </div>
              {theme === 'dark' && (
                <div className="w-4 h-4 rounded-full bg-sky-500 flex items-center justify-center text-white">
                  <Check className="w-2.5 h-2.5" />
                </div>
              )}
            </div>
            <div className="h-10 rounded-lg bg-slate-900 border border-slate-800 p-2 flex items-center gap-2">
              <div className="w-2 h-2 rounded-full bg-emerald-400" />
              <div className="h-2 w-16 bg-slate-800 rounded" />
              <div className="h-2 w-8 bg-sky-500/50 rounded ml-auto" />
            </div>
            <p className="text-[11px] text-slate-400 mt-2">
              Standard deep slate dark mode optimized for homelabs.
            </p>
          </div>

          {/* OLED Black (Preview) */}
          <div className="p-4 rounded-xl border border-slate-800/60 bg-slate-950/30 opacity-60">
            <div className="flex items-center justify-between mb-3">
              <div className="flex items-center gap-2">
                <div className="w-3.5 h-3.5 rounded-full bg-black border border-slate-800" />
                <span className="text-xs font-semibold text-slate-400">OLED Black</span>
              </div>
              <span className="text-[10px] font-mono px-1.5 py-0.5 rounded bg-slate-800 text-slate-500">
                Soon
              </span>
            </div>
            <div className="h-10 rounded-lg bg-black border border-slate-900 p-2 flex items-center gap-2">
              <div className="w-2 h-2 rounded-full bg-emerald-500" />
              <div className="h-2 w-16 bg-slate-900 rounded" />
            </div>
            <p className="text-[11px] text-slate-500 mt-2">
              Pure black contrast for high-efficiency OLED displays.
            </p>
          </div>

          {/* Light Theme (Preview) */}
          <div className="p-4 rounded-xl border border-slate-800/60 bg-slate-950/30 opacity-60">
            <div className="flex items-center justify-between mb-3">
              <div className="flex items-center gap-2">
                <div className="w-3.5 h-3.5 rounded-full bg-slate-100 border border-slate-300" />
                <span className="text-xs font-semibold text-slate-400">Clean Slate</span>
              </div>
              <span className="text-[10px] font-mono px-1.5 py-0.5 rounded bg-slate-800 text-slate-500">
                Soon
              </span>
            </div>
            <div className="h-10 rounded-lg bg-slate-100 border border-slate-200 p-2 flex items-center gap-2">
              <div className="w-2 h-2 rounded-full bg-emerald-600" />
              <div className="h-2 w-16 bg-slate-300 rounded" />
            </div>
            <p className="text-[11px] text-slate-500 mt-2">
              Bright modern theme for daytime workspace clarity.
            </p>
          </div>
        </div>
      </div>
    </div>
  );
};
