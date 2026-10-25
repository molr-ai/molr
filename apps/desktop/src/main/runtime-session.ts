type Notifications = { dispose(): void };

/** Owns one view's handshake, login permission and cleanup lifecycle. */
export class RuntimeSession<V, N extends Notifications> {
  view?: V;
  notifications?: N;
  private loaded = false;
  private ready = false;
  private initialLogin = false;
  private timer?: ReturnType<typeof setTimeout>;

  constructor(
    private readonly cancelOAuth: () => void,
    private readonly destroy: (view: V) => void,
  ) {}

  attach(view: V, notifications: N, initialLogin: boolean) {
    this.dispose();
    this.view = view;
    this.notifications = notifications;
    this.initialLogin = initialLogin;
  }

  navigate(view: V, keepInitialLogin: boolean, timeout: () => void) {
    if (this.view !== view) return;
    this.initialLogin &&= keepInitialLogin;
    this.notifications?.dispose();
    this.cancelOAuth();
    this.loaded = false;
    this.ready = false;
    clearTimeout(this.timer);
    this.timer = setTimeout(() => {
      if (this.view === view && !this.complete) timeout();
    }, 30_000);
  }

  didLoad(view: V) {
    if (this.view === view) this.loaded = true;
  }
  didHandshake() {
    if (this.view) this.ready = true;
  }
  get complete() {
    return !!this.view && this.loaded && this.ready;
  }
  reveal() {
    clearTimeout(this.timer);
  }
  consumeInitialLogin() {
    const allowed = !!this.view && this.initialLogin;
    this.initialLogin = false;
    return allowed;
  }

  dispose() {
    this.initialLogin = false;
    this.notifications?.dispose();
    this.notifications = undefined;
    this.cancelOAuth();
    clearTimeout(this.timer);
    this.loaded = false;
    this.ready = false;
    const old = this.view;
    this.view = undefined;
    if (old) this.destroy(old);
  }
}
