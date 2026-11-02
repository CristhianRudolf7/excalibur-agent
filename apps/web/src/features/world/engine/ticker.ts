/** Controls a private rendering ticker. Visibility, reduced motion and pending movement are independent inputs. */
export class TickerController {
  visible = true
  reducedMotion = false
  moving = false
  running = false
  private start: () => void
  private stop: () => void
  private notify: (running: boolean) => void
  constructor(start: () => void, stop: () => void, notify: (running: boolean) => void = () => {}) {
    this.start = start
    this.stop = stop
    this.notify = notify
  }
  update() {
    const running = this.visible && !this.reducedMotion && this.moving
    if (running === this.running) return
    this.running = running
    if (running) this.start()
    else this.stop()
    this.notify(running)
  }
  destroy() {
    this.moving = false
    this.visible = false
    this.update()
    this.stop()
  }
}
