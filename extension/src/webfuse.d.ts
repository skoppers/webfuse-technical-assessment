/**
 * Ambient typings for the subset of the Webfuse Extension API this extension uses.
 * Anything marked
 * "unverified" is a documented-but-unconfirmed detail; check at runtime.
 */

declare namespace WebfuseExt {
  interface MessageSender {
    tab?: Tab;
    frameId?: number;
    [key: string]: unknown;
  }

  type MessageListener = (
    message: unknown,
    sender: MessageSender,
    sendResponse?: (response: unknown) => void,
  ) => unknown;

  interface Runtime {
    sendMessage(message: unknown): Promise<unknown | null>;
    onMessage: {
      addListener(callback: MessageListener): void;
      removeListener(callback: MessageListener): void;
    };
  }

  interface Tab {
    active: boolean;
    sessionId: string;
    ssid: number;
    title: string;
    url: string;
    newTab: boolean;
    paused: boolean;
  }

  interface Tabs {
    sendMessage(tabId: number | null, message: unknown, options?: { frameId?: number }): Promise<unknown | null>;
    query(queryInfo?: object): Promise<Tab[]>;
    /** Only URL changes are reported. */
    onUpdated: {
      addListener(callback: (tabId: number, changeInfo: { url: string }, tab: Tab) => void): void;
    };
    onCreated: { addListener(callback: (tab: Tab) => void): void };
    onRemoved: { addListener(callback: (tabId: number, info: { isWindowClosing: boolean }) => void): void };
  }

  interface BrowserAction {
    openPopup(keepOpen?: boolean): void;
    closePopup(): void;
    resizePopup(width: number, height: number): void;
    setPopupStyles(styles: Record<string, string | number>): void;
    setPopupPosition(position: { top?: string; bottom?: string; left?: string; right?: string }): void;
    setPopupBadgeText(opts: { text: string; tabId?: number }): void;
  }

  interface Space {
    id: number;
    name: string;
    slug: string;
    domain: string;
    [key: string]: unknown;
  }

  interface SessionInfo {
    sessionId: string;
    opentokSessionId: string;
    space: Space;
    metadata: Record<string, unknown>;
  }

  /** Session events carry `event_type`; broadcasts carry `message` (unverified: example repo shows `{type:'message', data}`). */
  interface SessionPayload {
    event_type?: string;
    message?: unknown;
    data?: unknown;
    final_location?: string;
    [key: string]: unknown;
  }

  type SessionListener = (payload: SessionPayload, sender?: unknown) => void;

  interface WebfuseSession {
    env: Record<string, string | undefined>;
    getSessionInfo(): Promise<SessionInfo>;
    /** Session events + broadcasts. */
    on: { addListener(callback: SessionListener): void };
    /** Alternate spelling used in the extension guide (unverified which one fires). */
    onMessage?: { addListener(callback: SessionListener): void };
    broadcastMessage(message: unknown): void;
    log(entry: Record<string, unknown>): void;
  }

  interface Browser {
    runtime: Runtime;
    tabs: Tabs;
    /** Not available in content scripts. */
    browserAction: BrowserAction;
    webfuseSession: WebfuseSession;
  }
}

declare const browser: WebfuseExt.Browser;
declare const chrome: WebfuseExt.Browser;
