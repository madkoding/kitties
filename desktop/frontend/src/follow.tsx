import type { RefObject } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import { t } from "./i18n";

// How near the end (in pixels) still counts as being at it.
const NEAR = 48;

export interface Follow {
  // away is whether the person scrolled up from the end, and the list stopped following.
  away: boolean;
  // toEnd takes the list to its end and has it follow again.
  toEnd: () => void;
}

// useFollow keeps a scrolled list at its end while the person is there: what comes in (a message,
// a step, a picture that loads) shows without a scroll. When they scroll up to read, it stops
// following until they come back down, or tap the button that takes them there; a new key (another
// chat, another task) starts at the end again.
export function useFollow(box: RefObject<HTMLElement | null>, key: unknown): Follow {
  const [away, setAway] = useState(false);
  const stuck = useRef(true);
  const toEnd = () => {
    const el = box.current;
    stuck.current = true;
    setAway(false);
    if (el) {
      el.scrollTop = el.scrollHeight;
    }
  };
  useEffect(() => {
    toEnd();
  }, [key]);
  useEffect(() => {
    const el = box.current;
    if (!el) {
      return;
    }
    const keep = () => {
      if (stuck.current) {
        el.scrollTop = el.scrollHeight;
      }
    };
    const onScroll = () => {
      const at = el.scrollHeight - el.scrollTop - el.clientHeight <= NEAR;
      stuck.current = at;
      setAway(!at);
    };
    el.addEventListener("scroll", onScroll, { passive: true });
    // A picture that loads grows the list without a change to its nodes: load does not bubble,
    // so it is heard on the way down.
    el.addEventListener("load", keep, true);
    const mo = new MutationObserver(keep);
    mo.observe(el, { childList: true, subtree: true, characterData: true });
    const ro = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(keep);
    ro?.observe(el);
    return () => {
      el.removeEventListener("scroll", onScroll);
      el.removeEventListener("load", keep, true);
      mo.disconnect();
      ro?.disconnect();
    };
  }, [box.current]);
  return { away, toEnd };
}

// FollowButton is the way back to the end of a list the person scrolled up in.
export function FollowButton({ follow }: { follow: Follow }) {
  return follow.away ? (
    <button class="follow-jump" onClick={follow.toEnd} data-testid="follow-jump">
      <span aria-hidden="true">↓</span> {t("Go to the latest")}
    </button>
  ) : null;
}
