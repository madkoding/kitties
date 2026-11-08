import { useEffect, useRef } from "preact/hooks";

// Tabs are the sections of a dialog: pills that stay at the top while the dialog scrolls, wrap on
// a wide screen and slide sideways on a phone, the chosen one kept in view. The arrow keys, Home
// and End move between them, as in any tab list. Each tab is `<prefix>-tab-<id>`; with panels, it
// points at the panel `<prefix>-panel-<id>`.
export default function Tabs<T extends string>({
  tabs,
  tab,
  onPick,
  prefix,
  label,
  panels,
}: {
  tabs: { id: T; label: string }[];
  tab: T;
  onPick: (t: T) => void;
  prefix: string;
  label?: string;
  // panels says the dialog draws its sections as tab panels the tabs point at.
  panels?: boolean;
}) {
  const bar = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const on = bar.current?.querySelector<HTMLElement>('[aria-selected="true"]');
    on?.scrollIntoView?.({ block: "nearest", inline: "center" });
  }, [tab]);
  const onKey = (e: KeyboardEvent) => {
    const i = tabs.findIndex((x) => x.id === tab);
    const to =
      e.key === "ArrowRight"
        ? (i + 1) % tabs.length
        : e.key === "ArrowLeft"
          ? (i - 1 + tabs.length) % tabs.length
          : e.key === "Home"
            ? 0
            : e.key === "End"
              ? tabs.length - 1
              : -1;
    if (to < 0) {
      return;
    }
    e.preventDefault();
    onPick(tabs[to].id);
    bar.current?.querySelectorAll<HTMLElement>('[role="tab"]')[to]?.focus();
  };
  return (
    <div
      class="settings-tabs"
      role="tablist"
      aria-label={label}
      ref={bar}
      onKeyDown={onKey}
      data-testid={`${prefix}-tabs`}
    >
      {tabs.map((x) => (
        <button
          key={x.id}
          role="tab"
          id={`${prefix}-tab-${x.id}`}
          aria-selected={tab === x.id}
          aria-controls={panels ? `${prefix}-panel-${x.id}` : undefined}
          tabIndex={tab === x.id ? 0 : -1}
          class={`plain settings-tab ${tab === x.id ? "on" : ""}`}
          onClick={() => onPick(x.id)}
          data-testid={`${prefix}-tab-${x.id}`}
        >
          {x.label}
        </button>
      ))}
    </div>
  );
}
