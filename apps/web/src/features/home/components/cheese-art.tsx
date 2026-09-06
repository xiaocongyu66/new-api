/**
 * 品牌插图 — 一块奶酪切片。
 *
 * 纯内联 SVG 即可无需额外请求，并且继承当前主题（通过 Tailwind 颜色工具类，
 * 调色板的 amber 家族映射到 `styles/theme.css` 中的 cheese 系金 anchors）。
 *
 * 运动来自 `cheese-*` 类，它们在 `prefers-reduced-motion: reduce` 下自动关闭。
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
          background: 'radial-gradient(circle at center, rgba(251, 146, 60, 0.5), transparent 70%)'
        }}
      />
      <svg
        viewBox='0 0 360 300'
        className='cheese-float h-auto w-full max-w-[420px]'
        fill='none'
        xmlns='http://www.w3.org/2000/svg'
      >
        <defs>
          {/* Soft rim glow. A feSpecularLighting/fePointLight pair renders the
           * same idea, but React's SVG typings reject fePointLight as a child,
           * and the visual difference here is negligible. */}
          <filter id='cheeseShine' x='-20%' y='-20%' width='140%' height='140%'>
            <feGaussianBlur in='SourceAlpha' stdDeviation='4' result='blurred' />
            <feOffset dy='-2' in='blurred' result='lifted' />
            <feComposite
              in='lifted'
              in2='SourceAlpha'
              operator='out'
              result='rim'
            />
            <feMerge>
              <feMergeNode in='SourceGraphic' />
              <feMergeNode in='rim' />
            </feMerge>
          </filter>
        </defs>

        {/* Ground shadow */}
        <ellipse
          cx='180'
          cy='290'
          rx='140'
          ry='12'
          fill='rgba(0, 0, 0, 0.2)'
        />

        {/* Wedge: top rind plane + front face, drawn as a simple
            two-plane solid so it reads as 3D without a mesh. */}
        <path
          d='M90,210 L270,210 L270,120 C270,90 180,20 90,120 Z'
          fill='currentColor'
          className='text-amber-500 dark:text-amber-400'
        />

        <path
          d='M90,120 L180,60 L270,120'
          fill='currentColor'
          className='text-amber-600 dark:text-amber-500'
        />

        {/* Holes on the large front face. Each is a darker well plus a
            lighter inner disc so it reads as depth, not a flat dot. */}
        <g>
          <circle cx='120' cy='150' r='15' fill='rgba(0,0,0,0.3)' />
          <circle cx='120' cy='150' r='6' fill='rgba(255,255,255,0.5)' />
          <circle cx='240' cy='130' r='12' fill='rgba(0,0,0,0.25)' />
          <circle cx='240' cy='130' r='5' fill='rgba(255,255,255,0.4)' />
        </g>

        {/* A couple of holes cut into the narrow side face. */}
        <g>
          <path
            d='M100,130 L95,110 L105,110 Z'
            fill='rgba(0,0,0,0.2)'
          />
          <path
            d='M100,140 L95,120 L105,120 Z'
            fill='rgba(0,0,0,0.15)'
          />
        </g>

        {/* Highlight along the freshly cut top edge. */}
        <path
          d='M100,120 L200,120 L190,90 L110,90 Z'
          fill='rgba(255,255,255,0.3)'
          filter='url(#cheeseShine)'
        />

        {/* Sparkles — the "fresh out of the fridge" wink. */}
        <g className='fill-amber-400/80 dark:fill-amber-300/80'>
          <path d='M150,80 L152,75 L154,80 L152,85 Z' />
          <path d='M180,70 L182,65 L184,70 L182,75 Z' />
          <path d='M210,75 L212,70 L214,75 L212,80 Z' />
        </g>
      </svg>
    </div>
  )
}