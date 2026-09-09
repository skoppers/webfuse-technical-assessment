/**
 * Detects URL changes inside a single document: patches history.pushState/replaceState
 * and listens for popstate/hashchange on the window the Capture module was installed on.
 * Full-page loads are covered by the content script re-running on every load and
 * emitting the initial URL.
 */
export type NavigationChangeHandler = (url: string, title: string | undefined) => void;

export function installNavigationHooks(win: Window, onChange: NavigationChangeHandler): () => void {
  let lastUrl = win.location.href;

  const check = () => {
    const url = win.location.href;
    if (url === lastUrl) return;
    lastUrl = url;
    onChange(url, win.document.title || undefined);
  };

  const history = win.history;
  const originalPush = history.pushState;
  const originalReplace = history.replaceState;

  history.pushState = function (this: History, ...args: Parameters<History["pushState"]>) {
    const result = originalPush.apply(this, args);
    check();
    return result;
  };
  history.replaceState = function (this: History, ...args: Parameters<History["replaceState"]>) {
    const result = originalReplace.apply(this, args);
    check();
    return result;
  };
  win.addEventListener("popstate", check);
  win.addEventListener("hashchange", check);

  return () => {
    history.pushState = originalPush;
    history.replaceState = originalReplace;
    win.removeEventListener("popstate", check);
    win.removeEventListener("hashchange", check);
  };
}
