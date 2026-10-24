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
import { api, applyAuthBundle, isAuthBundle } from '@/lib/api'
import { useAuthStore, type AuthBundle } from '@/stores/auth-store'

/**
 * 跳过 dev 免登录开关。开发中想验证未登录态、登录页或权限拦截时，带上
 * `?dev-login=off`（或写入 localStorage）即可，不必停掉后端环境变量。
 */
const DISABLE_STORAGE_KEY = 'new-api:dev-auto-login-disabled'

const DISABLE_QUERY_KEY = 'dev-login'

function devAutoLoginDisabled(): boolean {
  try {
    if (localStorage.getItem(DISABLE_STORAGE_KEY) === '1') {
      return true
    }
    return new URLSearchParams(window.location.search).get(DISABLE_QUERY_KEY) === 'off'
  } catch {
    return false
  }
}

/**
 * root 的 beforeLoad 在每次导航都会跑，所以这里必须自己判断是否已有可用会话：
 * 否则每次跳转都会重新签一个 session，既把 users_sessions 表刷爆，也会触发
 * 后端的会话签发频率上限把自己锁在门外。
 */
function hasLiveSession(): boolean {
  const { accessToken, accessExpiresAt } = useAuthStore.getState().auth
  if (!accessToken || !accessExpiresAt) {
    return false
  }
  return accessExpiresAt > Math.floor(Date.now() / 1000) + 60
}

/**
 * dev 模式下自动登录：后端在 `dev` build tag 下才注册该路由，并要求显式设置
 * `DEV_AUTO_LOGIN`。生产构建既不注册也不响应，因此这里同时用
 * `import.meta.env.DEV` 兜底，双保险不会在线上生效。
 *
 * 失败一律静默返回 null——开发环境的免登录是便利设施，不该因为账号创建失败
 * 就挡住真实登录页。
 */
export async function devAutoLogin(): Promise<AuthBundle | null> {
  if (!import.meta.env.DEV) {
    return null
  }
  if (devAutoLoginDisabled()) {
    return null
  }
  if (hasLiveSession()) {
    return null
  }

  try {
    const res = await api.post(
      '/api/user/dev/auto-login',
      {},
      { skipAuthRefresh: true, skipErrorHandler: true }
    )
    if (!res.data?.success || !isAuthBundle(res.data.data)) {
      return null
    }
    const bundle = res.data.data
    applyAuthBundle(bundle, false)
    return bundle
  } catch {
    return null
  }
}
