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

// Bundle size budget gate for `apps/web`. Node ESM, node builtins only
// (fs, zlib, path) — no package imports, so it can run right after
// `bun run build` in CI without installing anything.
//
// Budgets: apps/web/scripts/bundle-budgets.json
//   entries { glob, maxGzipKb [, maxRawKb] [, label] }
//   entries { globs: [...], sum: true, maxGzipKb, label }  -> aggregate gzip across patterns
//
// Globs are matched against chunk basenames; `*` matches any run of
// characters (including dots), so `index.*.js` matches content-hashed
// names like `index.bec1d3b570.js`. `dist/static/js` is walked recursively,
// so chunks under `dist/static/js/async/` (e.g. the lazy-loaded vendor-codemirror chunk)
// are included too. A pattern that matches nothing (e.g. `vendor-codemirror` before its cache group lands)
// is reported clearly and does not fail the gate.

import { readFileSync, readdirSync, statSync } from 'node:fs'
import { gzipSync } from 'node:zlib'
import { basename, dirname, join } from 'node:path'

const SCRIPT_DIR = dirname(
  import.meta.url.startsWith('file://') ? import.meta.url.slice(7) : import.meta.url
)
const DIST_DIR = process.env.BUNDLE_BUDGET_DIST || join(SCRIPT_DIR, '..', 'dist', 'static', 'js')
const BUDGETS_PATH = join(SCRIPT_DIR, 'bundle-budgets.json')

function globToRegex(glob) {
  let re = ''
  for (let i = 0; i < glob.length; i++) {
    const ch = glob[i]
    if (ch === '*') {
      re += '.*'
    } else if ('.+*?()[]{}|^$\\'.includes(ch)) {
      re += '\\' + ch
    } else {
      re += ch
    }
  }
  return new RegExp(`^${re}$`)
}

function kib(bytes) {
  return (bytes / 1024).toFixed(1)
}

function pad(s, len) {
  return String(s).padEnd(len)
}

function main() {
  if (!statSync(DIST_DIR, { throwIfNoEntry: false })) {
    console.error(`budget gate: dist dir not found: ${DIST_DIR} (run bun run build first)`)
    return 1
  }

  const budgets = JSON.parse(readFileSync(BUDGETS_PATH, 'utf8'))
  const entries = budgets.entries || []
  const files = []
  const walk = (dir, prefix = '') => {
    for (const entry of readdirSync(dir, { withFileTypes: true })) {
      if (entry.isDirectory()) walk(join(dir, entry.name), prefix + entry.name + '/')
      else if (entry.name.endsWith('.js')) files.push(prefix + entry.name)
    }
  }
  walk(DIST_DIR)

  const compiled = []
  for (const entry of entries) {
    const patterns = entry.glob ? [entry.glob] : entry.globs || []
    compiled.push({
      ...entry,
      regexes: patterns.map(globToRegex),
    })
  }

  let failed = 0
  let checked = 0
  const rows = []

  for (const entry of compiled) {
    const matched = []
    for (const file of files) {
      const b = basename(file)
      if (entry.regexes.some((re) => re.test(b))) matched.push(file)
    }

    if (entry.sum) {
      let gzipSum = 0
      let rawSum = 0
      for (const file of matched) {
        const buf = readFileSync(join(DIST_DIR, file))
        rawSum += buf.length
        gzipSum += gzipSync(buf, { level: 9 }).length
      }
      checked++
      const ok = gzipSum <= entry.maxGzipKb * 1024
      if (!ok) failed++
      rows.push({
        label: entry.label,
        pattern: entry.globs.join(','),
        file: matched.length ? matched.join(', ') : '(none matched)',
        raw: kib(rawSum),
        gzip: kib(gzipSum),
        budget: String(entry.maxGzipKb),
        status: matched.length ? (ok ? 'PASS' : 'FAIL') : 'MISSING',
      })
      continue
    }

    if (matched.length === 0) {
      rows.push({
        label: entry.label,
        pattern: entry.glob,
        file: '(none matched)',
        raw: '-',
        gzip: '-',
        budget: String(entry.maxGzipKb),
        status: 'MISSING',
      })
      continue
    }

    for (const file of matched) {
      const buf = readFileSync(join(DIST_DIR, file))
      const raw = buf.length
      const gzip = gzipSync(buf, { level: 9 }).length
      checked++
      const ok = gzip <= entry.maxGzipKb * 1024
      if (!ok) failed++
      rows.push({
        label: entry.label,
        pattern: entry.glob,
        file,
        raw: kib(raw),
        gzip: kib(gzip),
        budget: String(entry.maxGzipKb),
        status: ok ? 'PASS' : 'FAIL',
      })
    }
  }

  const widths = {
    label: Math.max(...rows.map((r) => r.label.length), 'Label'.length),
    pattern: Math.max(...rows.map((r) => r.pattern.length), 'Pattern'.length),
    file: Math.max(...rows.map((r) => r.file.length), 'File'.length),
  }

  console.log(`bundle budget gate: ${DIST_DIR}`)
  console.log(
    pad('Label', widths.label) +
      '  ' +
      pad('Pattern', widths.pattern) +
      '  ' +
      pad('File', widths.file) +
      '  Raw kB  Gzip kB  Budget kB  Status'
  )
  console.log(
    '-'.repeat(widths.label) +
      '  ' +
      '-'.repeat(widths.pattern) +
      '  ' +
      '-'.repeat(widths.file) +
      '  -------  -------  ----------  ------'
  )
  for (const r of rows) {
    console.log(
      pad(r.label, widths.label) +
        '  ' +
        pad(r.pattern, widths.pattern) +
        '  ' +
        pad(r.file, widths.file) +
        `  ${pad(r.raw, 7)}  ${pad(r.gzip, 7)}  ${pad(r.budget, 10)}  ${r.status}`
    )
  }
  console.log(
    `bundle budget gate: ${checked} check(s), ${failed} breach(es) → ${
      failed ? 'FAIL' : 'PASS'
    }`
  )
  return failed ? 1 : 0
}

process.exitCode = main()
