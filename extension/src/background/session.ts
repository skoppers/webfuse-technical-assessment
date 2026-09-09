/**
 * Session identity from the platform. `sessionId` is expected to equal the webhook/REST
 * `session_id` — logged once so it can be compared on first run.
 */
export interface SessionContext {
  sessionId: string;
  spaceId: string;
  /** True when getSessionInfo failed and placeholders are in use. */
  placeholder: boolean;
}

export async function loadSessionContext(): Promise<SessionContext> {
  try {
    const info = await browser.webfuseSession.getSessionInfo();
    const ctx: SessionContext = {
      sessionId: String(info.sessionId),
      spaceId: String(info.space?.id ?? ""),
      placeholder: false,
    };
    console.info("[saa] session context", ctx);
    return ctx;
  } catch (err) {
    console.warn("[saa] getSessionInfo failed, using placeholder ids", err);
    return { sessionId: `local-${Date.now().toString(36)}`, spaceId: "", placeholder: true };
  }
}
