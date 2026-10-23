/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

// Editing model for the SMTP account pool. It lives apart from the editor
// component so the component file exports nothing but components, which is what
// Fast Refresh needs to reload the editor in place.

export type SMTPPoolAccount = {
  // id only exists so React can track a row while it is being edited; it is
  // never serialized into the stored option.
  id: string
  server: string
  port: string
  account: string
  token: string
  from: string
  ssl: boolean
  starttls: boolean
  dailyLimit: string
  priority: string
}

export const SECURITY_OPTIONS = [
  { value: 'starttls', ssl: false, starttls: true },
  { value: 'ssl', ssl: true, starttls: false },
  { value: 'none', ssl: false, starttls: false },
] as const

let nextPoolRowId = 0

export function newPoolRowId() {
  nextPoolRowId += 1
  return `pool-row-${nextPoolRowId}`
}

// serializePool renders the editable rows back into the option's JSON form.
//
// A blank token means "keep the stored credential": the value is carried over
// from `stored` for the same server/account pair. That keeps a password out of
// the page while still letting an admin edit the other fields, and changing the
// mailbox (server or account) intentionally drops the old credential instead of
// reusing it for a different host.
export function serializePool(
  accounts: SMTPPoolAccount[],
  stored: SMTPPoolAccount[] = []
): string {
  return JSON.stringify(
    accounts.map((account) => {
      const entry: Record<string, unknown> = {
        server: account.server.trim(),
        port: Number(account.port.trim() || '0'),
        account: account.account.trim(),
      }

      const token = account.token.trim() || storedToken(stored, account)
      if (token) entry.token = token

      if (account.from.trim()) entry.from = account.from.trim()
      if (account.ssl) entry.ssl = true
      if (account.starttls) entry.starttls = true
      if (account.dailyLimit.trim()) {
        entry.dailyLimit = Number(account.dailyLimit.trim())
      }
      entry.priority = Number(account.priority.trim() || '0')
      return entry
    })
  )
}

function storedToken(stored: SMTPPoolAccount[], account: SMTPPoolAccount) {
  const server = account.server.trim()
  const mailbox = account.account.trim()
  const match = stored.find(
    (candidate) =>
      candidate.server.trim() === server && candidate.account.trim() === mailbox
  )
  return match ? match.token.trim() : ''
}

// parsePool reads the stored JSON into editable rows. Rows that fail to parse
// are dropped rather than blocking the whole form, so an admin can still see
// and fix the valid accounts.
export function parsePool(value: string): SMTPPoolAccount[] {
  if (!value.trim()) return []
  try {
    const parsed = JSON.parse(value)
    if (!Array.isArray(parsed)) return []
    return parsed
      .filter(
        (entry): entry is Record<string, unknown> =>
          typeof entry === 'object' && entry !== null
      )
      .map((entry) => ({
        id: newPoolRowId(),
        server: typeof entry.server === 'string' ? entry.server : '',
        port: entry.port != null ? String(entry.port) : '587',
        account: typeof entry.account === 'string' ? entry.account : '',
        token: typeof entry.token === 'string' ? entry.token : '',
        from: typeof entry.from === 'string' ? entry.from : '',
        ssl: entry.ssl === true,
        starttls: entry.starttls === true,
        dailyLimit:
          typeof entry.dailyLimit === 'number' ? String(entry.dailyLimit) : '',
        priority:
          typeof entry.priority === 'number' ? String(entry.priority) : '0',
      }))
  } catch {
    return []
  }
}
