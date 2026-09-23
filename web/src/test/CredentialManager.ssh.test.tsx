import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { CredentialManager } from '../components/CredentialManager'
import type { ConnFields } from '../components/ConnectionBar'
import type { DialogsApi } from '../components/dialogs'
import { TooltipProvider } from '../components/ui/tooltip'
import { PasswordTextarea } from '../components/ui/textarea'

const fields: ConnFields = { host: 'db.internal', port: '5432', user: 'postgres', password: '', dbname: 'app', sslmode: 'verify-full' }
const dialogs: DialogsApi = {
  confirm: vi.fn().mockResolvedValue(true),
  prompt: vi.fn().mockResolvedValue(null),
  promptNullable: vi.fn().mockResolvedValue(null),
  form: vi.fn().mockResolvedValue(null),
}

describe('CredentialManager SSH profile settings', () => {
  it('masks a multiline private key until explicitly revealed', () => {
    render(<PasswordTextarea aria-label="SSH private key" value="private-key-material" onChange={vi.fn()} />)
    const key = screen.getByLabelText('SSH private key')
    expect(key.className).toContain('[-webkit-text-security:disc]')
    fireEvent.click(screen.getByRole('button', { name: 'Show secret text' }))
    expect(key.className).not.toContain('[-webkit-text-security:disc]')
  })

  it('keeps SSH configuration in Advanced and saves only the selected auth fields', async () => {
    const onSave = vi.fn().mockResolvedValue('profile-ssh')
    const setFields = vi.fn()
    render(<TooltipProvider><CredentialManager
      fields={fields} setFields={setFields} saved={[]}
      onConnect={vi.fn().mockResolvedValue('session-1')}
      onTest={vi.fn().mockResolvedValue({ database: 'app' })}
      onSave={onSave} onDuplicate={vi.fn().mockResolvedValue('profile-copy')} onDelete={vi.fn().mockResolvedValue(undefined)}
      vault={{ exists: true, unlocked: true }} onVaultAction={vi.fn().mockResolvedValue(undefined)} dialogs={dialogs}
      autoLogin={false} onAutoLogin={vi.fn()} sessions={[]} activeId="" onSwitch={vi.fn()}
      onDisconnectOne={vi.fn()} deadIds={{}} onReconnectOne={vi.fn()} onReconnectAll={vi.fn()}
    /></TooltipProvider>)

    expect(screen.queryByLabelText('SSH host')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('switch', { name: 'Advanced connection form' }))
    fireEvent.click(screen.getByRole('switch', { name: 'Use SSH tunnel' }))
    expect(screen.getByLabelText('SSH host')).toBeInTheDocument()
    expect(screen.getByLabelText('SSH password')).toHaveAttribute('type', 'password')
    fireEvent.change(screen.getByLabelText('Profile name'), { target: { value: 'Prod through bastion' } })
    fireEvent.change(screen.getByLabelText('SSH host'), { target: { value: 'bastion.internal' } })
    fireEvent.change(screen.getByLabelText('SSH username'), { target: { value: 'deploy' } })
    fireEvent.change(screen.getByLabelText('SSH password'), { target: { value: 'secret' } })
    fireEvent.click(screen.getByRole('button', { name: /save/i }))

    await waitFor(() => expect(onSave).toHaveBeenCalledTimes(1))
    const metadata = onSave.mock.calls[0][3]
    expect(metadata).toMatchObject({ ssh_enabled: true, ssh_host: 'bastion.internal', ssh_user: 'deploy', ssh_auth_method: 'password', ssh_password: 'secret' })
    await waitFor(() => expect(screen.getByLabelText('SSH password')).toHaveValue(''))
  })

  it('prompts for one-time SSH credentials when the vault has no saved secret', async () => {
    const onConnect = vi.fn()
      .mockRejectedValueOnce(new Error('no SSH credentials saved for this profile; enter them in Advanced settings'))
      .mockResolvedValueOnce('session-1')
    vi.mocked(dialogs.form).mockResolvedValueOnce({ password: 'one-time-password' })
    render(<TooltipProvider><CredentialManager
      fields={fields} setFields={vi.fn()} saved={[{ id: 'profile-ssh', name: 'Secure', host: fields.host, port: fields.port, user: fields.user, password: '', dbname: fields.dbname, sslmode: fields.sslmode, options: { ssh_enabled: true, ssh_auth_method: 'password' } }]}
      onConnect={onConnect} onTest={vi.fn().mockResolvedValue({})} onSave={vi.fn().mockResolvedValue('profile-ssh')}
      onDuplicate={vi.fn().mockResolvedValue('profile-copy')} onDelete={vi.fn().mockResolvedValue(undefined)}
      vault={{ exists: true, unlocked: false }} onVaultAction={vi.fn().mockResolvedValue(undefined)} dialogs={dialogs}
      autoLogin={false} onAutoLogin={vi.fn()} sessions={[]} activeId="" onSwitch={vi.fn()}
      onDisconnectOne={vi.fn()} deadIds={{}} onReconnectOne={vi.fn()} onReconnectAll={vi.fn()}
    /></TooltipProvider>)

    fireEvent.click(screen.getByRole('button', { name: /secure/i }))
    fireEvent.click(screen.getByRole('button', { name: /^connect$/i }))
    await waitFor(() => expect(onConnect).toHaveBeenCalledTimes(2))
    expect(onConnect).toHaveBeenLastCalledWith('profile-ssh', { password: 'one-time-password', private_key: undefined, passphrase: undefined })
    expect(dialogs.form).toHaveBeenCalledWith(expect.objectContaining({ title: 'Enter SSH credentials' }))
  })

  it('verifies and pins an untrusted host key before retrying with one-time credentials', async () => {
    const onConnect = vi.fn()
      .mockRejectedValueOnce(new Error('no SSH credentials saved for this profile; enter them in Advanced settings'))
      .mockRejectedValueOnce(new Error('SSH host key is not trusted; fingerprint: SHA256:known-fingerprint'))
      .mockResolvedValueOnce('session-1')
    const onSave = vi.fn().mockResolvedValue('profile-ssh')
    vi.mocked(dialogs.form).mockResolvedValueOnce({ password: 'one-time-password' })
    vi.mocked(dialogs.confirm).mockResolvedValueOnce(true)
    render(<TooltipProvider><CredentialManager
      fields={fields} setFields={vi.fn()} saved={[{ id: 'profile-ssh', name: 'Secure', host: fields.host, port: fields.port, user: fields.user, password: '', dbname: fields.dbname, sslmode: fields.sslmode, options: { ssh_enabled: true, ssh_auth_method: 'password' } }]}
      onConnect={onConnect} onTest={vi.fn().mockResolvedValue({})} onSave={onSave}
      onDuplicate={vi.fn().mockResolvedValue('profile-copy')} onDelete={vi.fn().mockResolvedValue(undefined)}
      vault={{ exists: true, unlocked: false }} onVaultAction={vi.fn().mockResolvedValue(undefined)} dialogs={dialogs}
      autoLogin={false} onAutoLogin={vi.fn()} sessions={[]} activeId="" onSwitch={vi.fn()}
      onDisconnectOne={vi.fn()} deadIds={{}} onReconnectOne={vi.fn()} onReconnectAll={vi.fn()}
    /></TooltipProvider>)

    fireEvent.click(screen.getByRole('button', { name: /secure/i }))
    fireEvent.click(screen.getByRole('button', { name: /^connect$/i }))
    await waitFor(() => expect(onConnect).toHaveBeenCalledTimes(3))
    expect(dialogs.confirm).toHaveBeenCalledWith(expect.objectContaining({ title: 'Verify SSH server identity' }))
    expect(onSave.mock.calls[0][3]).toMatchObject({ ssh_host_key: 'SHA256:known-fingerprint', ssh_password: '' })
    expect(onConnect).toHaveBeenLastCalledWith('profile-ssh', { password: 'one-time-password', private_key: undefined, passphrase: undefined })
  })
})
