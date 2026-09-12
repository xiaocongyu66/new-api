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
import { Mail, Globe, MessageCircle, Send } from 'lucide-react'
import { SiGithub, SiDiscord, SiQq } from 'react-icons/si'

export interface StatusInfo {
  github_oauth?: boolean
  discord_oauth?: boolean
  oidc_enabled?: boolean
  wechat_login?: boolean
  telegram_oauth?: boolean
  linuxdo_oauth?: boolean
  custom_oauth_providers?: Array<{
    id: string
    name: string
    icon?: string
  }>
}

export interface BuiltinBinding {
  /** Path parameter for DELETE /api/user/:id/bindings/:binding_type */
  key: string
  /** Field on the user payload holding the bound identifier */
  field: string
  label: string
  icon: React.ReactNode
  statusKey: keyof StatusInfo | null
}

// key must match the provider names the Go ClearBinding accepts, which are not
// the users-table column names — those are `field`. QQ is the odd one out: it
// lives in the qq_bindings table and only the admin single-user endpoint
// surfaces it, as qq_open_id.
export const BUILTIN_BINDINGS: ReadonlyArray<BuiltinBinding> = [
  {
    key: 'email',
    field: 'email',
    label: 'Email',
    icon: <Mail className='h-4 w-4' />,
    statusKey: null,
  },
  {
    key: 'github',
    field: 'github_id',
    label: 'GitHub',
    icon: <SiGithub className='h-4 w-4' />,
    statusKey: 'github_oauth',
  },
  {
    key: 'discord',
    field: 'discord_id',
    label: 'Discord',
    icon: <SiDiscord className='h-4 w-4' />,
    statusKey: 'discord_oauth',
  },
  {
    key: 'wechat',
    field: 'wechat_id',
    label: 'WeChat',
    icon: <MessageCircle className='h-4 w-4' />,
    statusKey: 'wechat_login',
  },
  {
    key: 'oidc',
    field: 'oidc_id',
    label: 'OIDC',
    icon: <Globe className='h-4 w-4' />,
    statusKey: 'oidc_enabled',
  },
  {
    key: 'telegram',
    field: 'telegram_id',
    label: 'Telegram',
    icon: <Send className='h-4 w-4' />,
    statusKey: 'telegram_oauth',
  },
  {
    key: 'linuxdo',
    field: 'linux_do_id',
    label: 'LinuxDO',
    icon: <Globe className='h-4 w-4' />,
    statusKey: 'linuxdo_oauth',
  },
  {
    key: 'qq',
    field: 'qq_open_id',
    label: 'QQ',
    icon: <SiQq className='h-4 w-4' />,
    statusKey: null,
  },
]
