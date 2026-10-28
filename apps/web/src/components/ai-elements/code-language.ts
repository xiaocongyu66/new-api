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

const LANGUAGE_ALIASES: Record<string, string> = {
  csharp: 'c#',
  golang: 'go',
  js: 'javascript',
  shell: 'bash',
  shellscript: 'bash',
  ts: 'typescript',
}

const LANGUAGE_PATTERN = /^[a-z0-9][a-z0-9+#._-]{0,31}$/i

// Normalize a fenced-code language label onto the name used for display and
// CodeMirror language selection. Unknown or unsafe labels fall back to
// plaintext instead of reaching the editor.
export function getRequestedCodeLanguage(language?: string) {
  const normalized = language?.trim().toLowerCase() || 'plaintext'
  if (!LANGUAGE_PATTERN.test(normalized)) {
    return 'plaintext'
  }

  return LANGUAGE_ALIASES[normalized] ?? normalized
}
