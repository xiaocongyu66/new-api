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
 * Brand illustration — chibi little mouse and a wedge of cheese, top-down.
 *
 * Palette lifted from the reference artwork the maintainer supplied:
 * warm tan/orange-brown fur (#d3ac7f / #af632a), thick chocolate outlines
 * (#553222), and a cheese wedge drawn as one coherent oblique prism —
 * bright cut face #f4cb5e, top slope #dfa93f, shaded end #c68b3a, plain
 * holes (#b5762f, five, no outlines), one cream sheen stroke (#fbeecd),
 * and a warm halo div behind the wedge. Few tones, hole-forward.
 *
 * The scene tells one story: the mouse WANTS this cheese. Star-struck
 * eyes, whiskers and paws stretched toward the wedge's point, crumbs
 * already nibbled off, hearts rising in the gap. Motion is a shared 5s
 * duet (`cheese-lean` / `cheese-desire` / `cheese-jiggle` in
 * styles/index.css): the mouse strains toward the wedge, the hearts pop,
 * and the cheese answers with a happy squash-jiggle. Everything collapses
 * to a no-op under `prefers-reduced-motion: reduce`.
 */

export function CheeseArt(props: { className?: string }) {
  return (
    <div
      className={`relative ${props.className ?? ''}`}
      role='img'
      aria-label='A chibi little mouse gazing longingly at a golden wedge of cheese, seen from above'
    >
      {/* Warm glow behind the scene. */}
      <div
        aria-hidden
        className='cheese-aurora absolute inset-0 -z-10 rounded-full opacity-60 blur-3xl dark:opacity-30'
        style={{
          background:
            'radial-gradient(circle at 50% 55%, oklch(0.85 0.16 85 / 65%), transparent 70%)',
        }}
      />
      {/* Focused halo so the wedge pops — strongest in dark mode. */}
      <div
        aria-hidden
        className='absolute -z-10 rounded-full opacity-40 blur-2xl dark:opacity-80'
        style={{
          right: '0%',
          top: '14%',
          width: '64%',
          height: '74%',
          background:
            'radial-gradient(closest-side, oklch(0.87 0.14 90 / 75%), transparent 72%)',
        }}
      />
      <svg
        viewBox='0 0 360 300'
        className='cheese-float h-auto w-full max-w-[420px]'
        fill='none'
        xmlns='http://www.w3.org/2000/svg'
      >
        {/* Contact shadow under the whole scene */}
        <ellipse
          cx='180'
          cy='258'
          rx='150'
          ry='13'
          className='fill-[#553222]'
          opacity='0.12'
        />

        {/* ── Little mouse, top view — the whole body leans toward the
            cheese every few seconds (cheese-lean) ────────────────────── */}
        <g className='cheese-lean'>
          {/* Curly tail (drawn first, sways from its base) */}
          <g className='cheese-tail'>
            <path
              d='M96 202 C 68 228 38 216 30 246 C 27 258 38 264 46 255'
              className='stroke-[#553222]'
              strokeWidth='13'
              strokeLinecap='round'
            />
            <path
              d='M96 202 C 68 228 38 216 30 246 C 27 258 38 264 46 255'
              className='stroke-[#af632a]'
              strokeWidth='6'
              strokeLinecap='round'
            />
          </g>
          {/* Tiny back peeking below the head */}
          <ellipse
            cx='102'
            cy='204'
            rx='30'
            ry='16'
            className='fill-[#d3ac7f] stroke-[#553222]'
            strokeWidth='5'
          />
          {/* Big chibi ears */}
          <g className='cheese-ear'>
            <circle
              cx='74'
              cy='102'
              r='34'
              className='fill-[#af632a] stroke-[#553222]'
              strokeWidth='5'
            />
            <circle cx='74' cy='102' r='18' className='fill-[#f3ebd4]' />
          </g>
          <circle
            cx='160'
            cy='98'
            r='34'
            className='fill-[#af632a] stroke-[#553222]'
            strokeWidth='5'
          />
          <circle cx='160' cy='98' r='18' className='fill-[#f3ebd4]' />
          {/* Huge head + snout reaching for the cheese */}
          <path
            d='M62 152 C 62 116 94 98 122 100 C 152 102 172 124 176 146 C 178 155 190 155 199 161 C 191 170 179 170 176 177 C 168 200 142 212 116 210 C 84 208 62 188 62 152 Z'
            className='fill-[#d3ac7f] stroke-[#553222]'
            strokeWidth='5'
            strokeLinejoin='round'
          />
          {/* Blush */}
          <ellipse
            cx='106'
            cy='176'
            rx='11'
            ry='6.5'
            className='fill-[#e8998d]'
            opacity='0.55'
          />
          <ellipse
            cx='162'
            cy='166'
            rx='10'
            ry='6'
            className='fill-[#e8998d]'
            opacity='0.55'
          />
          {/* Big glossy eyes — star-struck by the cheese */}
          <ellipse
            cx='136'
            cy='140'
            rx='9.5'
            ry='11.5'
            className='fill-[#553222]'
          />
          <path
            d='M138.5 131 L139.8 134.2 L143 135.5 L139.8 136.8 L138.5 140 L137.2 136.8 L134 135.5 L137.2 134.2 Z'
            className='fill-white'
          />
          <circle
            cx='134'
            cy='144'
            r='1.4'
            className='fill-white'
            opacity='0.8'
          />
          <ellipse
            cx='161'
            cy='134'
            rx='9.5'
            ry='11.5'
            className='fill-[#553222]'
          />
          <path
            d='M163.5 125 L164.8 128.2 L168 129.5 L164.8 130.8 L163.5 134 L162.2 130.8 L159 129.5 L162.2 128.2 Z'
            className='fill-white'
          />
          <circle
            cx='159'
            cy='138'
            r='1.4'
            className='fill-white'
            opacity='0.8'
          />
          {/* Nose */}
          <circle cx='196' cy='161' r='7' className='fill-[#553222]' />
          <circle
            cx='194'
            cy='159'
            r='2'
            className='fill-white'
            opacity='0.7'
          />
          {/* Whiskers, pointing at the wedge's tip */}
          <g
            className='stroke-[#553222]'
            strokeWidth='2.2'
            strokeLinecap='round'
            opacity='0.85'
          >
            <path d='M178 148 Q 200 140 220 140' />
            <path d='M182 161 Q 206 160 226 164' />
            <path d='M178 172 Q 198 182 216 192' />
          </g>
          {/* Tiny front paws reaching for the cheese */}
          <ellipse
            cx='150'
            cy='198'
            rx='13'
            ry='9'
            className='fill-[#f3ebd4] stroke-[#553222]'
            strokeWidth='4.5'
          />
          <ellipse
            cx='174'
            cy='190'
            rx='13'
            ry='9'
            className='fill-[#f3ebd4] stroke-[#553222]'
            strokeWidth='4.5'
          />
        </g>
        {/* ── Cheese wedge — one coherent oblique prism: the cut face is
            a doorstop triangle aimed at the mouse, the top slope and the
            far end share the same depth vector, so every edge meets where
            it should. The whole wedge answers the lean with a jiggle ── */}
        <g className='cheese-jiggle'>
          {/* Far end (darkest — faces away from the light) */}
          <path
            d='M312 100 L322 236 L352 220 L342 84 Z'
            className='fill-[#c68b3a] stroke-[#553222]'
            strokeWidth='3'
            strokeLinejoin='round'
          />
          {/* Top slope receding back-right from the ridge (mid tone) */}
          <path
            d='M234 168 L312 100 L342 84 L264 152 Z'
            className='fill-[#dfa93f] stroke-[#553222]'
            strokeWidth='3'
            strokeLinejoin='round'
          />
          {/* Cut face (bright tone) with the point at the mouse's nose */}
          <path
            d='M242 159 L305 107 Q312 100 313 110 L321 226 Q322 236 314 230 L226 162 Q234 168 242 159 Z'
            className='fill-[#f4cb5e] stroke-[#553222]'
            strokeWidth='5'
            strokeLinejoin='round'
          />
          {/* Five plain holes — the signature of cheese */}
          <ellipse
            cx='276'
            cy='156'
            rx='15'
            ry='13'
            className='fill-[#b5762f]'
            transform='rotate(-12 276 156)'
          />
          <ellipse
            cx='288'
            cy='196'
            rx='10'
            ry='8.5'
            className='fill-[#b5762f]'
            transform='rotate(8 288 196)'
          />
          <ellipse cx='300' cy='124' rx='7' ry='6' className='fill-[#b5762f]' />
          <ellipse cx='300' cy='172' rx='8' ry='7' className='fill-[#b5762f]' />
          <ellipse cx='254' cy='174' rx='6' ry='5' className='fill-[#b5762f]' />
          {/* One bold sheen stroke hugging the ridge */}
          <path
            d='M246 162 Q276 131 302 106'
            className='stroke-[#fbeecd]'
            strokeWidth='5'
            strokeLinecap='round'
            opacity='0.7'
          />
        </g>

        {/* Crumbs already nibbled off the point */}
        <path
          d='M206 198 L213 193 L217 200 L210 205 Z'
          className='fill-[#f4cb5e] stroke-[#553222]'
          strokeWidth='3'
          strokeLinejoin='round'
        />
        <path
          d='M216 182 L221 179 L223 184 L218 187 Z'
          className='fill-[#f4cb5e] stroke-[#553222]'
          strokeWidth='2.5'
          strokeLinejoin='round'
        />

        {/* Hearts rising in the gap — the wanting itself */}
        <path
          d='M207 115 C 205.5 112.5 202 113.2 202 116 C 202 118.6 204.8 120.6 207 123 C 209.2 120.6 212 118.6 212 116 C 212 113.2 208.5 112.5 207 115 Z'
          className='cheese-desire fill-[#e8998d] stroke-[#553222]'
          strokeWidth='2'
        />
        <path
          d='M222 96 C 220.8 94 218 94.6 218 96.8 C 218 98.8 220.2 100.6 222 102.4 C 223.8 100.6 226 98.8 226 96.8 C 226 94.6 223.2 94 222 96 Z'
          className='cheese-desire fill-[#e8998d] stroke-[#553222]'
          strokeWidth='2'
          style={{ animationDelay: '800ms' }}
        />

        {/* ── Sparkles ───────────────────────────────────────────────── */}
        <g className='fill-[#af632a]' opacity='0.75'>
          <circle
            cx='332'
            cy='58'
            r='5'
            className='cheese-twinkle'
            style={{ animationDelay: '0ms' }}
          />
          <circle
            cx='354'
            cy='120'
            r='3.5'
            className='cheese-twinkle'
            style={{ animationDelay: '600ms' }}
          />
          <circle
            cx='30'
            cy='58'
            r='4'
            className='cheese-twinkle'
            style={{ animationDelay: '1200ms' }}
          />
          <circle
            cx='320'
            cy='252'
            r='3'
            className='cheese-twinkle'
            style={{ animationDelay: '900ms' }}
          />
          <circle
            cx='36'
            cy='246'
            r='3'
            className='cheese-twinkle'
            style={{ animationDelay: '1700ms' }}
          />
        </g>
      </svg>
    </div>
  )
}
