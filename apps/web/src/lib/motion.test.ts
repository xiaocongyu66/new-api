import { describe, expect, it } from 'bun:test'
import {
  CARD_ITEM_VARIANTS,
  MOTION_TRANSITION,
  MOTION_VARIANTS,
  STAGGER_VARIANTS,
} from './motion'

describe('motion contracts', () => {
  it('keeps the shared transition presets stable', () => {
    expect(MOTION_TRANSITION.default).toEqual({
      duration: 0.25,
      ease: [0.33, 1, 0.68, 1],
    })
    expect(MOTION_TRANSITION.fast).toEqual({
      duration: 0.15,
      ease: [0.33, 1, 0.68, 1],
    })
    expect(MOTION_TRANSITION.spring).toEqual({
      type: 'spring',
      damping: 20,
      stiffness: 300,
    })
    expect(MOTION_TRANSITION.none).toEqual({ duration: 0 })
  })

  it('keeps enter and exit states available for shared variants', () => {
    expect(MOTION_VARIANTS.pageEnter).toEqual({
      initial: { opacity: 0, y: 8, filter: 'blur(4px)' },
      animate: { opacity: 1, y: 0, filter: 'blur(0px)' },
      exit: { opacity: 0, y: -4, filter: 'blur(2px)' },
    })
    expect(MOTION_VARIANTS.fadeIn.exit).toEqual({ opacity: 0 })
    expect(MOTION_VARIANTS.scaleIn.exit).toEqual({ opacity: 0, scale: 0.96 })
  })

  it('keeps stagger and card item contracts compatible with motion/react', () => {
    expect(STAGGER_VARIANTS.animate.transition).toEqual({ staggerChildren: 0.04 })
    expect(CARD_ITEM_VARIANTS.initial).toEqual({
      opacity: 0,
      y: 12,
      scale: 0.98,
    })
    expect(CARD_ITEM_VARIANTS.animate.transition).toEqual(MOTION_TRANSITION.default)
  })
})
