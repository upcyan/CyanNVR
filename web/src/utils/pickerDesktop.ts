import type { ObjectDirective } from 'vue'

// Vant handles touch gestures itself. Desktop gestures select its public option
// elements, so normal change/confirm events and disabled options stay consistent.
const cleanup = new WeakMap<HTMLElement, () => void>()
export const pickerDesktop: ObjectDirective<HTMLElement> = {
  mounted(root) {
    let drag: { column: HTMLElement; y: number; moved: boolean } | null = null
    let suppressClick = false
    let wheelTotal = 0
    let wheelColumn: HTMLElement | null = null
    let wheelTime = 0
    const columnAt = (target: EventTarget | null) =>
      target instanceof Element ? target.closest<HTMLElement>('.van-picker-column') : null
    const move = (column: HTMLElement, delta: number) => {
      const items = [...column.querySelectorAll<HTMLElement>('.van-picker-column__item')]
      const current = items.findIndex(item => item.classList.contains('van-picker-column__item--selected'))
      let index = current + delta
      while (index >= 0 && index < items.length) {
        if (!items[index].classList.contains('van-picker-column__item--disabled')) {
          items[index].click()
          return
        }
        index += Math.sign(delta)
      }
    }
    const wheel = (event: WheelEvent) => {
      const column = columnAt(event.target)
      if (!column || !event.deltaY) return
      event.preventDefault()
      if (wheelColumn !== column || Date.now() - wheelTime > 200) wheelTotal = 0
      wheelColumn = column
      wheelTime = Date.now()
      wheelTotal += event.deltaY * (event.deltaMode === 1 ? 16 : event.deltaMode === 2 ? column.clientHeight : 1)
      if (Math.abs(wheelTotal) >= 32) {
        move(column, Math.sign(wheelTotal))
        wheelTotal = 0
      }
    }
    const down = (event: PointerEvent) => {
      if (event.pointerType !== 'mouse' || event.button !== 0) return
      const column = columnAt(event.target)
      if (!column) return
      suppressClick = false
      drag = { column, y: event.clientY, moved: false }
    }
    const pointerMove = (event: PointerEvent) => {
      if (!drag || !(event.buttons & 1)) return
      const height = drag.column.querySelector<HTMLElement>('.van-picker-column__item')?.offsetHeight || 44
      const distance = event.clientY - drag.y
      if (Math.abs(distance) < height / 2) return
      event.preventDefault()
      move(drag.column, distance < 0 ? 1 : -1)
      drag.y = event.clientY
      drag.moved = true
    }
    const up = () => {
      suppressClick = drag?.moved || false
      drag = null
    }
    const click = (event: MouseEvent) => {
      if (suppressClick && event.isTrusted && columnAt(event.target)) {
        event.preventDefault()
        event.stopImmediatePropagation()
        suppressClick = false
      }
    }
    const key = (event: KeyboardEvent) => {
      const column = columnAt(event.target)
      if (!column) return
      if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
        event.preventDefault()
        move(column, event.key === 'ArrowDown' ? 1 : -1)
      } else if (event.key === 'Enter' || event.key === ' ') {
        event.preventDefault()
        ;(event.target as HTMLElement).click()
      }
    }
    root.addEventListener('wheel', wheel, { passive: false })
    root.addEventListener('pointerdown', down)
    window.addEventListener('pointermove', pointerMove)
    window.addEventListener('pointerup', up)
    window.addEventListener('pointercancel', up)
    root.addEventListener('click', click, true)
    root.addEventListener('keydown', key)
    cleanup.set(root, () => {
      root.removeEventListener('wheel', wheel)
      root.removeEventListener('pointerdown', down)
      window.removeEventListener('pointermove', pointerMove)
      window.removeEventListener('pointerup', up)
      window.removeEventListener('pointercancel', up)
      root.removeEventListener('click', click, true)
      root.removeEventListener('keydown', key)
    })
  },
  unmounted(root) {
    cleanup.get(root)?.()
    cleanup.delete(root)
  },
}
