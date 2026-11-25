/**
 * Accounts on the server, for administrators: add people, change their role,
 * set a new password, and disable or re-enable an account. Nobody can sign
 * up on their own once the first account exists, so this is how people join.
 */
import { useState } from 'react';
import type { FormEvent, MouseEvent } from 'react';
import { KeyRound, UserPlus } from 'lucide-react';

import { createUser, listUsers, updateUser } from '../../api/queries';
import type { User } from '../../api/types';
import { useResource } from '../../api/resources';
import { useAuth } from '../../auth/authContext';
import { errorMessage } from '../../api/media';
import { Dialog } from '../Dialog';
import { Menu } from '../ui/Menu';
import type { MenuAnchor } from '../ui/Menu';
import { useToast } from '../ui/Toast';
import './UsersPanel.css';

const MIN_PASSWORD = 8;

function AddUserDialog({ onClose, onAdded }: { onClose: () => void; onAdded: () => void }) {
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [role, setRole] = useState<'user' | 'admin'>('user');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const toast = useToast();

  const submit = async (e?: FormEvent) => {
    e?.preventDefault();
    if (password.length < MIN_PASSWORD) {
      setError(`The password needs at least ${MIN_PASSWORD} characters.`);
      return;
    }
    setBusy(true);
    setError('');
    try {
      await createUser({ username: username.trim(), password, role });
      toast({ message: `Added ${username.trim()}. They can sign in now.`, tone: 'success' });
      onAdded();
      onClose();
    } catch (err) {
      const msg = errorMessage(err);
      setError(msg === 'That resource already exists.' ? 'That username is taken.' : msg);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog
      open
      title="Add a user"
      onClose={onClose}
      dismissible={!busy}
      footer={
        <>
          <button type="button" className="button" onClick={onClose} disabled={busy}>
            Cancel
          </button>
          <button type="button" className="primary-button" onClick={() => void submit()} disabled={busy || !username.trim() || !password}>
            {busy ? 'Adding…' : 'Add user'}
          </button>
        </>
      }
    >
      <form className="users-form" onSubmit={(e) => void submit(e)}>
        <label className="field">
          <span className="field-label">Username</span>
          <input autoFocus autoComplete="off" value={username} onChange={(e) => setUsername(e.target.value)} />
          <span className="field-hint">Letters, digits, dots, dashes and underscores.</span>
        </label>
        <label className="field">
          <span className="field-label">Password</span>
          <input type="password" autoComplete="new-password" value={password} onChange={(e) => setPassword(e.target.value)} />
          <span className="field-hint">At least {MIN_PASSWORD} characters. Share it with them privately.</span>
        </label>
        <fieldset className="users-role">
          <legend className="field-label">Role</legend>
          <label>
            <input type="radio" name="role" checked={role === 'user'} onChange={() => setRole('user')} />
            <span>
              <strong>Member</strong>
              <span className="field-hint">Watches the libraries you give them access to.</span>
            </span>
          </label>
          <label>
            <input type="radio" name="role" checked={role === 'admin'} onChange={() => setRole('admin')} />
            <span>
              <strong>Administrator</strong>
              <span className="field-hint">Manages libraries, users, metadata and backups.</span>
            </span>
          </label>
        </fieldset>
        {error && (
          <p className="error-text" role="alert">
            {error}
          </p>
        )}
      </form>
    </Dialog>
  );
}

function PasswordDialog({ user, onClose }: { user: User; onClose: () => void }) {
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const toast = useToast();
  const submit = async (e?: FormEvent) => {
    e?.preventDefault();
    if (password.length < MIN_PASSWORD) {
      setError(`The password needs at least ${MIN_PASSWORD} characters.`);
      return;
    }
    setBusy(true);
    try {
      await updateUser(user.id, { password });
      toast({ message: `New password set for ${user.username}. They were signed out everywhere.`, tone: 'success' });
      onClose();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog
      open
      title={`New password for ${user.username}`}
      onClose={onClose}
      footer={
        <>
          <button type="button" className="button" onClick={onClose}>
            Cancel
          </button>
          <button type="button" className="primary-button" onClick={() => void submit()} disabled={busy || !password}>
            Set password
          </button>
        </>
      }
    >
      <form className="users-form" onSubmit={(e) => void submit(e)}>
        <label className="field">
          <span className="field-label">New password</span>
          <input type="password" autoFocus autoComplete="new-password" value={password} onChange={(e) => setPassword(e.target.value)} />
          <span className="field-hint">They'll be signed out on every device and sign in with this.</span>
        </label>
        {error && <p className="error-text">{error}</p>}
      </form>
    </Dialog>
  );
}

export function UsersPanel() {
  const { user: me } = useAuth();
  const users = useResource(listUsers, []);
  const toast = useToast();
  const [adding, setAdding] = useState(false);
  const [passwordFor, setPasswordFor] = useState<User | null>(null);
  const [menu, setMenu] = useState<{ user: User; anchor: MenuAnchor } | null>(null);

  const patch = async (u: User, p: { role?: string; enabled?: boolean }, done: string) => {
    try {
      await updateUser(u.id, p);
      toast({ message: done, tone: 'success' });
      users.reload();
    } catch (err) {
      toast({ message: errorMessage(err), tone: 'error' });
    }
  };

  const openMenu = (u: User, e: MouseEvent<HTMLElement>, fromButton = false) => {
    e.preventDefault();
    setMenu({ user: u, anchor: fromButton || (e.clientX === 0 && e.clientY === 0) ? { element: e.currentTarget } : { x: e.clientX, y: e.clientY } });
  };

  const itemsFor = (u: User) => {
    const self = u.id === me?.id;
    const enabled = u.enabled !== false;
    return [
      { id: 'password', label: 'Set a new password…', onSelect: () => setPasswordFor(u) },
      ...(self
        ? []
        : [
            u.role === 'admin'
              ? { id: 'role', label: 'Make a member', onSelect: () => void patch(u, { role: 'user' }, `${u.username} is now a member.`) }
              : { id: 'role', label: 'Make an administrator', onSelect: () => void patch(u, { role: 'admin' }, `${u.username} is now an administrator.`) },
            enabled
              ? { id: 'disable', label: 'Disable account', danger: true, onSelect: () => void patch(u, { enabled: false }, `${u.username} can no longer sign in.`) }
              : { id: 'enable', label: 'Enable account', onSelect: () => void patch(u, { enabled: true }, `${u.username} can sign in again.`) },
          ]),
    ];
  };

  return (
    <section className="settings-section">
      <header>
        <h2>Users</h2>
        <p>People can't sign up on their own. Add them here and give them access to libraries under Users & access.</p>
      </header>
      <div className="settings-rows">
        <div>
          <button type="button" className="primary-button" onClick={() => setAdding(true)}>
            <UserPlus size={17} /> Add user
          </button>
        </div>
        {users.loading && <p className="muted">Loading…</p>}
        {users.error && <p className="error-text">{users.error}</p>}
        {users.data && (
          <ul className="users-list">
            {users.data.users.map((u) => {
              const enabled = u.enabled !== false;
              return (
                <li key={u.id} className="users-row" data-disabled={!enabled} onContextMenu={(e) => openMenu(u, e)}>
                  <span className="users-avatar" aria-hidden="true">
                    {u.username.slice(0, 1).toUpperCase()}
                  </span>
                  <span className="users-name">
                    {u.username}
                    {u.id === me?.id && <span className="muted"> (you)</span>}
                  </span>
                  <span className={`users-role-chip role-${u.role}`}>{u.role === 'admin' ? 'Administrator' : 'Member'}</span>
                  {!enabled && <span className="users-disabled">Disabled</span>}
                  <button type="button" className="icon-button" onClick={() => setPasswordFor(u)} aria-label={`Set a new password for ${u.username}`} title="Set a new password">
                    <KeyRound size={16} />
                  </button>
                  <button type="button" className="button button-sm" onClick={(e) => openMenu(u, e, true)} aria-haspopup="menu">
                    Manage
                  </button>
                </li>
              );
            })}
          </ul>
        )}
      </div>
      {menu && <Menu anchor={menu.anchor} items={itemsFor(menu.user)} label={`Manage ${menu.user.username}`} onClose={() => setMenu(null)} />}
      {adding && <AddUserDialog onClose={() => setAdding(false)} onAdded={users.reload} />}
      {passwordFor && <PasswordDialog user={passwordFor} onClose={() => setPasswordFor(null)} />}
    </section>
  );
}
