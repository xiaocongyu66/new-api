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
/**
 * Brand illustration — a wedge of cheese.
 *
 * Pure inline SVG so it costs no extra request and inherits the active
 * theme through Tailwind color utilities (the palette's amber family maps
 * onto the cheese gold anchors in `styles/theme.css`).
 *
 * Motion comes from the `cheese-*` classes in `styles/index.css`, all of
 * which collapse to no-ops under `prefers-reduced-motion: reduce`.
 */
export function CheeseArt(props: { className?: string }) {
  return (
    <div
      className={`relative ${props.className ?? ''}`}
      role='img'
      aria-label='A wedge of cheese'
    >
      {/* Warm glow behind the wedge. */}
      <div
        aria-hidden
        className='cheese-aurora absolute inset-0 -z-10 rounded-full opacity-60 blur-3xl dark:opacity-30'
        style={{
          background:
            'radial-gradient(circle at 50% 55%, oklch(0.85 0.16 85 / 65%), transparent 70%)',
        }}
      />
      <svg
        viewBox='0 0 360 300'
        className='cheese-float h-auto w-full max-w-[420px]'
        fill='none'
        xmlns='http://www.w3.org/2000/svg'
      >
        <defs>
          <linearGradient id='cheese-face' x1='70' y1='80' x2='280' y2='250'>
            <stop offset='0%' stopColor='oklch(0.93 0.12 92)' />
            <stop offset='55%' stopColor='oklch(0.86 0.16 85)' />
            <stop offset='100%' stopColor='oklch(0.77 0.16 72)' />
          </linearGradient>
          <linearGradient id='cheese-top' x1='70' y1='60' x2='280' y2='120'>
            <stop offset='0%' stopColor='oklch(0.96 0.08 95)' />
            <stop offset='100%' stopColor='oklch(0.89 0.13 88)' />
          </linearGradient>
        </defs>

        {/* Ground shadow */}
        <ellipse
          cx='180'
          cy='276'
          rx='128'
          ry='13'
          className='fill-amber-900/10 dark:fill-amber-950/40'
        />

        {/* Wedge: top rind plane + front face, drawn as a simple
            two-plane solid so it reads as 3D without a mesh. */}
        <path
          d='M60 96 L246 52 L300 92 L104 140 Z'
          fill='url(#cheese-top)'
          className='stroke-amber-600/60 dark:stroke-amber-700'
          strokeWidth='5'
          strokeLinejoin='round'
        />
        <path
          d='M60 96 L104 140 L104 236 L60 196 Z'
          fill='url(#cheese-face)'
          className='stroke-amber-600/60 dark:stroke-amber-700'
          strokeWidth='5'
          strokeLinejoin='round'
        />
        <path
          d='M104 140 L300 92 L300 190 L104 236 Z'
          fill='url(#cheese-face)'
          className='stroke-amber-600/60 dark:stroke-amber-700'
          strokeWidth='5'
          strokeLinejoin='round'
        />

        {/* Holes on the large front face. Each is a darker well plus a
            lighter inner disc so it reads as depth, not a flat dot. */}
        <g>
          <ellipse
            cx='166'
            cy='170'
            rx='23'
            ry='19'
            className='fill-amber-700/30 dark:fill-amber-900/45'
          />
          <ellipse
            cx='166'
            cy='167'
            rx='18'
            ry='14'
            className='fill-amber-500/40 dark:fill-amber-600/40'
          />
          <ellipse
            cx='248'
            cy='146'
            rx='16'
            ry='13'
            className='fill-amber-700/30 dark:fill-amber-900/45'
          />
          <ellipse
            cx='248'
            cy='143'
            rx='12'
            ry='9'
            className='fill-amber-500/40 dark:fill-amber-600/40'
          />
          <ellipse
            cx='198'
            cy='215'
            rx='15'
            ry='12'
            className='fill-amber-700/30 dark:fill-amber-900/45'
          />
          <ellipse
            cx='198'
            cy='212'
            rx='11'
            ry='8'
            className='fill-amber-500/40 dark:fill-amber-600/40'
          />
          <circle
            cx='272'
            cy='176'
            r='8'
            className='fill-amber-700/30 dark:fill-amber-900/45'
          />
          <circle
            cx='272'
            cy='174'
            r='6'
            className='fill-amber-500/40 dark:fill-amber-600/40'
          />
        </g>

        {/* A couple of holes cut into the narrow side face. */}
        <g>
          <ellipse
            cx='82'
            cy='168'
            rx='9'
            ry='13'
            className='fill-amber-700/30 dark:fill-amber-900/45'
          />
          <ellipse
            cx='82'
            cy='166'
            rx='6'
            ry='9'
            className='fill-amber-500/35 dark:fill-amber-600/35'
          />
        </g>

        {/* Highlight along the freshly cut top edge. */}
        <path
          d='M60 96 L246 52 L252 58 L66 102 Z'
          className='fill-white/45 dark:fill-white/12'
        />

        {/* Sparkles — the "fresh out of the fridge" wink. */}
        <g className='fill-amber-400/80 dark:fill-amber-300/80'>
          <circle
            cx='318'
            cy='68'
            r='5'
            className='cheese-twinkle'
            style={{ animationDelay: '0ms' }}
          />
          <circle
            cx='338'
            cy='104'
            r='3.5'
            className='cheese-twinkle'
            style={{ animationDelay: '600ms' }}
          />
          <circle
            cx='36'
            cy='72'
            r='4'
            className='cheese-twinkle'
            style={{ animationDelay: '1200ms' }}
          />
          <circle
            cx='300'
            cy='232'
            r='3'
            className='cheese-twinkle'
            style={{ animationDelay: '900ms' }}
          />
          <circle
            cx='44'
            cy='224'
            r='3'
            className='cheese-twinkle'
            style={{ animationDelay: '1700ms' }}
          />
        </g>
      </svg>
    </div>
  )
}