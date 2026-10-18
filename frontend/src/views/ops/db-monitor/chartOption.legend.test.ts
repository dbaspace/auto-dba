import { describe, expect, it } from 'vitest'
import { LEGEND_VAL_COL_MIN_PX, LEGEND_VAL_COL_PX, computeLegendLayout } from './chartOption'

describe('computeLegendLayout', () => {
  it('keeps full value columns beside the plot on wide cards', () => {
    const layout = computeLegendLayout(980)
    expect(layout.stacked).toBe(false)
    expect(layout.valColWidth).toBe(LEGEND_VAL_COL_PX)
    expect(layout.legendWidth).toBeLessThanOrEqual(980 - 180)
  })

  it('never shrinks the current column below the header on half-width cards', () => {
    const layout = computeLegendLayout(520)
    expect(layout.valColWidth).toBeGreaterThanOrEqual(LEGEND_VAL_COL_MIN_PX)
    if (!layout.stacked) {
      expect(layout.legendWidth).toBeLessThanOrEqual(520 - 180)
    }
  })

  it('stacks the legend under the plot when the card cannot fit current', () => {
    const layout = computeLegendLayout(420)
    expect(layout.stacked).toBe(true)
    expect(layout.valColWidth).toBe(LEGEND_VAL_COL_PX)
    expect(layout.gridRight).toBe(16)
  })
})
