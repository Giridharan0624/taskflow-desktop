import { useState, useEffect, useMemo, useRef } from "preact/hooks";
import type { Task, StartTimerData } from "../app";
import { Button } from "./ui/Button";
import { Input } from "./ui/Input";
import { cn } from "../lib/cn";
import { recordHistory, suggestHistory } from "../lib/descriptionHistory";
import { recordTaskUse, topRecentTaskIds } from "../lib/taskHistory";
import { colorForProject } from "../lib/projectColor";

interface TaskSelectorProps {
  onStart: (data: StartTimerData) => void;
  loading: boolean;
  /** When true, the selector is shown beneath an already-running
   *  timer (Switch Task mode). We surface a small inline notice
   *  before the user starts, so they're aware the current session
   *  will be ended. Empty/false = stopped state, no notice needed. */
  switching?: { currentTaskTitle: string; runningSince: string } | null;
}

export function TaskSelector({ onStart, loading, switching }: TaskSelectorProps) {
  const [tasks, setTasks] = useState<Task[]>([]);
  const [selectedSource, setSelectedSource] = useState("");
  const [selectedTaskId, setSelectedTaskId] = useState("");
  const [description, setDescription] = useState("");
  const [fetchError, setFetchError] = useState("");
  const [refreshing, setRefreshing] = useState(false);
  // descriptionFocused gates the autocomplete suggestions popover.
  // Stays true on input → selection happens via mousedown which fires
  // BEFORE blur, so the click registers without the panel disappearing.
  const [descriptionFocused, setDescriptionFocused] = useState(false);
  const descInputRef = useRef<HTMLInputElement>(null);

  // Unified fetcher used on mount AND by the refresh button. Clears
  // any stale selection that no longer matches the returned list so
  // the dropdown doesn't keep displaying a now-unassigned task. The
  // Go client already falls back to the on-disk cache when the
  // network is down, so this works offline too.
  async function loadTasks() {
    setRefreshing(true);
    try {
      const r = (await window.go.main.App.GetMyTasks()) || [];
      setTasks(r);
      setFetchError("");
      // Reconcile selection — if the previously-selected task or
      // project is no longer in the list (unassigned, closed),
      // clear so the user picks again rather than starting a
      // timer against a stale id.
      setSelectedSource((prev) =>
        prev && r.some((t) => t.projectId === prev) ? prev : ""
      );
      setSelectedTaskId((prev) =>
        prev && r.some((t) => t.taskId === prev) ? prev : ""
      );
    } catch {
      setFetchError("Failed to load tasks");
    } finally {
      setRefreshing(false);
    }
  }

  useEffect(() => {
    loadTasks();
    // Intentional empty dep list — mount-once. loadTasks is stable
    // enough (closes over setters only).
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const projects = useMemo(() => {
    const map: Record<string, { id: string; name: string; tasks: Task[] }> = {};
    for (const task of tasks) {
      const pid = task.projectId || "direct";
      if (pid === "DIRECT" || pid === "direct") continue;
      if (!map[pid])
        map[pid] = { id: pid, name: task.projectName || "Project", tasks: [] };
      map[pid].tasks.push(task);
    }
    return Object.values(map);
  }, [tasks]);

  const sourceTasks = useMemo(
    () => projects.find((p) => p.id === selectedSource)?.tasks || [],
    [selectedSource, projects]
  );

  const selectedTask = tasks.find((t) => t.taskId === selectedTaskId);

  // When the user picks a task and the description is empty, autofill
  // it with the task title so they don't have to retype the obvious.
  // They can edit before pressing Start. We only autofill on the
  // empty→non-empty transition; once the user has typed anything, we
  // never overwrite.
  useEffect(() => {
    if (selectedTask && !description.trim()) {
      setDescription(selectedTask.title);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedTaskId]);

  function handleStartTask(e: Event) {
    e.preventDefault();
    if (!selectedTask) return;
    // Trim so a whitespace-only description doesn't pass canStartTask
    // (which uses the raw value) and then fail server-side validation
    // with an opaque error. See H-FE-3.
    const trimmed = description.trim();
    if (!trimmed) return;
    // Record the description for future autocomplete BEFORE clearing
    // the input — recordHistory dedupes case-insensitively and caps
    // the buffer at 25.
    recordHistory(trimmed);
    // Bump this task's use-count so it surfaces near the top of the
    // task dropdown next time. P2-21.
    recordTaskUse(selectedTask.taskId);
    onStart({
      taskId: selectedTask.taskId,
      projectId: selectedTask.projectId,
      taskTitle: selectedTask.title,
      projectName: selectedTask.projectName || "",
      description: trimmed,
    });
    setDescription("");
    setSelectedSource("");
    setSelectedTaskId("");
    setDescriptionFocused(false);
  }

  function handleMeeting() {
    const trimmed = description.trim();
    if (trimmed) recordHistory(trimmed);
    onStart({
      taskId: "",
      projectId: "",
      taskTitle: "Meeting",
      projectName: "",
      description: trimmed || "Meeting",
    });
    setDescription("");
    setDescriptionFocused(false);
  }

  // canStartTask uses the trimmed description so the "Start" button
  // correctly disables on whitespace-only input (H-FE-3).
  const canStartTask = description.trim().length > 0 && selectedTaskId;

  // Recent-description suggestions. Recomputed when the input
  // changes; callers see a fresh snapshot of localStorage rather than
  // a stale module-level cache. Limit 5 — anything longer crowds the
  // dropdown in the small window.
  const suggestions = useMemo(
    () => suggestHistory(description, 5),
    [description, descriptionFocused],
  );
  const showSuggestions =
    descriptionFocused &&
    suggestions.length > 0 &&
    // Hide if the only suggestion is identical to what's already typed.
    !(suggestions.length === 1 && suggestions[0] === description.trim());

  return (
    <div class="w-full space-y-2 min-w-0">
      {/* Description input with leading pencil glyph — gives the field a
          visual anchor and reads as "describe your work" rather than a
          bare text box. The icon sits inside the input via padding +
          absolute positioning. */}
      <div class="relative">
        <span
          class="pointer-events-none absolute left-2.5 top-[18px] -translate-y-1/2 text-muted-foreground/60"
          aria-hidden="true"
        >
          <svg class="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
            <path d="M12 20h9" />
            <path d="M16.5 3.5a2.121 2.121 0 0 1 3 3L7 19l-4 1 1-4Z" />
          </svg>
        </span>
        <Input
          ref={descInputRef}
          type="text"
          placeholder="What are you working on?"
          value={description}
          maxLength={500}
          onInput={(e) => setDescription((e.target as HTMLInputElement).value)}
          onFocus={() => setDescriptionFocused(true)}
          onBlur={() => {
            // Defer so a click on a suggestion (which fires mousedown
            // → blur → click) still has time to update state. 120 ms
            // is well within human click latency but well below
            // perceived sluggishness.
            setTimeout(() => setDescriptionFocused(false), 120);
          }}
          class="pl-8"
          aria-autocomplete="list"
          aria-expanded={showSuggestions}
        />

        {/* Autocomplete suggestions — recent descriptions, MRU-first.
            mousedown is intentional: it fires BEFORE blur, so the
            click registers before the panel disappears. */}
        {showSuggestions && (
          <div
            role="listbox"
            class="absolute z-40 left-0 right-0 mt-1 rounded-md border border-border bg-popover text-popover-foreground shadow-lg overflow-hidden max-h-44 overflow-y-auto"
          >
            <p class="px-2.5 py-1 text-[9px] font-semibold uppercase tracking-[0.10em] text-muted-foreground/70 border-b border-border/60">
              Recent
            </p>
            {suggestions.map((s, i) => (
              <button
                key={i}
                type="button"
                role="option"
                onMouseDown={(e) => {
                  e.preventDefault(); // keep focus on the input
                  setDescription(s);
                  setDescriptionFocused(false);
                  descInputRef.current?.focus();
                }}
                class={cn(
                  "w-full text-left px-2.5 py-1.5 text-xs leading-tight",
                  "text-foreground hover:bg-accent hover:text-accent-foreground transition-colors",
                  "focus-visible:outline-none focus-visible:bg-accent",
                  "flex items-center gap-2",
                )}
                title={s}
              >
                <svg class="h-3 w-3 flex-shrink-0 text-muted-foreground/70" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
                  <path d="M3 12a9 9 0 1 0 3-6.7L3 8" /><path d="M3 3v5h5" /><path d="M12 7v5l3 2" />
                </svg>
                <span class="flex-1 truncate">{s}</span>
              </button>
            ))}
          </div>
        )}
      </div>

      <div class="flex gap-1.5 min-w-0">
        <Dropdown
          value={selectedSource}
          placeholder="Select Project"
          icon={<ProjectIcon />}
          searchPlaceholder="Search projects…"
          options={projects.map((p) => ({
            value: p.id,
            label: p.name,
            swatch: colorForProject(p.name),
          }))}
          onChange={(v) => { setSelectedSource(v); setSelectedTaskId(""); }}
        />

        {selectedSource && (
          <Dropdown
            value={selectedTaskId}
            placeholder="Select Task"
            icon={<TaskIcon />}
            searchPlaceholder="Search tasks…"
            options={(() => {
              // Pin the user's most-used recent tasks (across the
              // whole task list, not just this project) — but only
              // the ones that actually live under the currently-
              // selected project, since switching projects changes
              // the visible task list. P2-21.
              const recent = new Set(topRecentTaskIds(5));
              return sourceTasks.map((t) => ({
                value: t.taskId,
                label: t.title,
                pinned: recent.has(t.taskId),
              }));
            })()}
            onChange={setSelectedTaskId}
          />
        )}

        {/* Refresh — fixed 36×36, same border treatment as the dropdowns
            so it reads as a peer of the row rather than a bolt-on. */}
        <button
          type="button"
          onClick={loadTasks}
          disabled={refreshing}
          title={refreshing ? "Refreshing…" : "Refresh tasks and projects"}
          aria-label="Refresh tasks"
          class={cn(
            "flex-shrink-0 h-9 w-9 rounded-md border border-input bg-background",
            "text-muted-foreground shadow-sm transition-all duration-150",
            "hover:border-ring/40 hover:text-foreground hover:bg-accent/30",
            "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background",
            "active:scale-[.96]",
            "disabled:opacity-60 disabled:cursor-not-allowed",
            "flex items-center justify-center"
          )}
        >
          <RefreshIcon spinning={refreshing} />
        </button>
      </div>

      {fetchError && (
        // The error is itself the recovery surface — clicking the
        // chip retries `loadTasks()`. Beats forcing the user to
        // hunt for the refresh button after a failed load.
        <button
          type="button"
          role="alert"
          onClick={loadTasks}
          disabled={refreshing}
          class={cn(
            "w-full flex items-center gap-1.5 rounded-md border border-destructive/25 bg-destructive/[0.07] px-2 py-1.5",
            "text-[10.5px] font-medium text-destructive leading-tight",
            "transition-colors hover:bg-destructive/[0.10] hover:border-destructive/35",
            "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-destructive/30",
            "disabled:opacity-60 disabled:cursor-not-allowed",
          )}
        >
          <svg class="h-3 w-3 flex-shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <circle cx="12" cy="12" r="10" />
            <path d="M12 8v4M12 16h.01" />
          </svg>
          <span class="flex-1 text-left">{fetchError}</span>
          <span class="flex items-center gap-1 text-[10px] opacity-90">
            <RefreshIcon spinning={refreshing} />
            {refreshing ? "Retrying" : "Retry"}
          </span>
        </button>
      )}

      {/* Switch-task warning. Surfaced ONLY when a timer is already
          running AND the user has started picking a new task — we
          don't want to badger them while they're just casually
          glancing at the strip. The elapsed string is a soft signal,
          not a confirmation modal: clicking Start still proceeds
          immediately, but they've seen the cost. */}
      {switching && (selectedTaskId || description.trim()) && (
        <p class="flex items-start gap-1.5 px-2 py-1.5 rounded-md bg-amber-500/[0.10] border border-amber-500/25 text-[10.5px] leading-snug text-amber-700 dark:text-amber-300">
          <svg class="mt-px h-3 w-3 flex-shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <path d="M12 9v4M12 17h.01" />
            <path d="M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0Z" />
          </svg>
          <span class="flex-1">
            This will stop <span class="font-semibold">{switching.currentTaskTitle}</span>
            {switching.runningSince ? (
              <> at <span class="font-semibold tabular-nums">{formatElapsedSince(switching.runningSince)}</span>.</>
            ) : (
              <> and start the new task.</>
            )}
          </span>
        </p>
      )}

      {/* Action row — Start dominates (60% width), Meeting is the
          secondary affordance (40%). Previously both were flex-1 which
          read as "two equal options" and buried the primary intent. */}
      <div class="flex gap-1.5">
        <Button
          type="button"
          class={cn(
            "h-9 font-semibold gap-1.5 shadow-sm hover:shadow",
            "flex-[3]",
          )}
          disabled={loading || !canStartTask}
          onClick={handleStartTask}
        >
          {loading ? (
            <span class="opacity-80">Starting…</span>
          ) : (
            <>
              <PlayIcon />
              Start
            </>
          )}
        </Button>
        <Button
          type="button"
          variant="outline"
          class="h-9 font-medium gap-1.5 flex-[2] text-[12.5px]"
          disabled={loading || !description}
          onClick={handleMeeting}
          title="Log a meeting (no specific task)"
        >
          <MeetingIcon />
          Meeting
        </Button>
      </div>
    </div>
  );
}

/* ═══ Helpers ═══ */

// formatElapsedSince renders the time elapsed since `iso` as
// "1h 23m" / "4m". Coarse on purpose — the switch-task notice is
// informational, not a stopwatch.
function formatElapsedSince(iso: string): string {
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return "";
  const elapsedMin = Math.max(0, Math.floor((Date.now() - t) / 60000));
  const h = Math.floor(elapsedMin / 60);
  const m = elapsedMin % 60;
  return h > 0 ? `${h}h ${m}m` : `${m}m`;
}

/* ═══ Custom Dropdown ═══ */

interface DropdownOption {
  value: string;
  label: string;
  /** Optional decoration rendered before the label — currently used
   *  to show a per-project color dot. */
  swatch?: string;
  /** When true, this option is rendered above the others under a
   *  "Recent" subhead. Used by the task dropdown to surface the
   *  user's most-used tasks. P2-21. */
  pinned?: boolean;
}

function Dropdown({
  value,
  placeholder,
  icon,
  options,
  onChange,
  searchPlaceholder = "Search…",
}: {
  value: string;
  placeholder: string;
  icon?: any;
  options: DropdownOption[];
  onChange: (v: string) => void;
  searchPlaceholder?: string;
}) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const ref = useRef<HTMLDivElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  const selected = options.find((o) => o.value === value);

  // Close on outside click
  useEffect(() => {
    if (!open) return;
    function handler(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) {
        setOpen(false);
      }
    }
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, [open]);

  // Reset the search query when the menu closes so the next open
  // starts fresh. Also focus the search input on open so the user
  // can start typing immediately. P2-20.
  useEffect(() => {
    if (open) {
      // Defer one tick — the input doesn't exist until after this
      // effect runs in the same render pass.
      setTimeout(() => searchRef.current?.focus(), 0);
    } else {
      setQuery("");
    }
  }, [open]);

  // Filter + partition. Query is case-insensitive substring match.
  // Pinned options stay pinned across filtering — if the user
  // searches "ref" and a pinned task matches, it shows under the
  // Recent header; non-pinned matches show below.
  const q = query.trim().toLowerCase();
  const filtered = q
    ? options.filter((o) => o.label.toLowerCase().includes(q))
    : options;
  const pinned = filtered.filter((o) => o.pinned);
  const rest = filtered.filter((o) => !o.pinned);
  const showSearch = options.length > 8 || q.length > 0;

  return (
    // min-w-0 is mandatory here. flex-1 alone defaults to
    // min-width:auto (= content width), so a dropdown containing a
    // long label like "Update Figma component" refuses to shrink and
    // pushes its sibling + itself past the window's right edge. With
    // min-w-0, flex-1 actually shares space equally and the
    // trigger's own `truncate` / marquee takes over.
    <div class="relative flex-1 min-w-0" ref={ref}>
      <button
        type="button"
        class={cn(
          "w-full flex items-center gap-2 px-3 h-9 rounded-md text-xs text-left",
          "bg-background border shadow-sm transition-all",
          "hover:border-ring/50",
          "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background",
          open ? "border-primary" : "border-input",
          selected ? "text-foreground font-medium" : "text-muted-foreground",
        )}
        onClick={() => setOpen(!open)}
        aria-expanded={open}
        aria-haspopup="listbox"
      >
        {icon && <span class="flex-shrink-0 text-muted-foreground">{icon}</span>}
        <span
          class="flex-1 min-w-0 truncate"
          title={selected ? selected.label : undefined}
        >
          {selected ? selected.label : placeholder}
        </span>
        <ChevronIcon open={open} />
      </button>

      {open && (
        <div
          role="listbox"
          // The TaskSelector lives in the bottom strip of the
          // window so opening downward would clip below the footer.
          // Opens UPWARD (bottom-full + mb-1) regardless of how
          // many options the user has. max-h-56 fits the search
          // input + ~6 rows; longer lists scroll internally.
          class="absolute z-50 bottom-full mb-1 w-full rounded-md border border-border bg-popover text-popover-foreground shadow-lg overflow-hidden flex flex-col max-h-56"
        >
          {/* Search row — surfaces only when there's enough content
              to make scrolling annoying, OR while the user is
              actively typing. P2-20. */}
          {showSearch && (
            <div class="px-2 py-1.5 border-b border-border/60 bg-muted/40 flex items-center gap-1.5 flex-shrink-0">
              <svg class="h-3 w-3 text-muted-foreground/80 flex-shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
                <circle cx="11" cy="11" r="7" />
                <path d="M21 21l-4.3-4.3" />
              </svg>
              <input
                ref={searchRef}
                type="text"
                value={query}
                onInput={(e) => setQuery((e.target as HTMLInputElement).value)}
                placeholder={searchPlaceholder}
                class="w-full bg-transparent text-[11.5px] text-foreground placeholder:text-muted-foreground/70 focus:outline-none"
                onKeyDown={(e) => {
                  if (e.key === "Escape") {
                    e.preventDefault();
                    setOpen(false);
                  }
                }}
              />
              {query && (
                <button
                  type="button"
                  onClick={() => setQuery("")}
                  class="text-muted-foreground/70 hover:text-foreground text-[10px]"
                  aria-label="Clear search"
                  title="Clear"
                >
                  ×
                </button>
              )}
            </div>
          )}

          <div class="overflow-y-auto flex-1">
            {filtered.length === 0 ? (
              <div class="px-3 py-3 text-[11.5px] text-center text-muted-foreground">
                {q ? `No matches for "${query}"` : "No options"}
              </div>
            ) : (
              <>
                {pinned.length > 0 && (
                  <>
                    <p class="px-3 pt-1.5 pb-1 text-[8.5px] font-semibold uppercase tracking-[0.10em] text-muted-foreground/70">
                      Recent
                    </p>
                    {pinned.map((opt) => (
                      <DropdownRow
                        key={opt.value}
                        opt={opt}
                        selected={opt.value === value}
                        onChoose={() => {
                          onChange(opt.value);
                          setOpen(false);
                        }}
                      />
                    ))}
                    {rest.length > 0 && (
                      <div class="my-0.5 border-t border-border/50" role="separator" />
                    )}
                  </>
                )}
                {rest.map((opt) => (
                  <DropdownRow
                    key={opt.value}
                    opt={opt}
                    selected={opt.value === value}
                    onChoose={() => {
                      onChange(opt.value);
                      setOpen(false);
                    }}
                  />
                ))}
              </>
            )}
          </div>
        </div>
      )}
    </div>
  );
}

function DropdownRow({
  opt,
  selected,
  onChoose,
}: {
  opt: DropdownOption;
  selected: boolean;
  onChoose: () => void;
}) {
  return (
    <button
      role="option"
      type="button"
      title={opt.label}
      aria-selected={selected}
      class={cn(
        "w-full text-left px-3 py-2 text-xs transition-colors",
        "focus-visible:outline-none focus-visible:bg-accent focus-visible:text-accent-foreground",
        selected
          ? "bg-primary/10 text-primary font-medium"
          : "text-foreground hover:bg-accent hover:text-accent-foreground",
      )}
      onClick={onChoose}
    >
      <div class="flex items-center gap-2 min-w-0">
        {selected ? (
          <svg class="w-3 h-3 flex-shrink-0" fill="currentColor" viewBox="0 0 20 20" aria-hidden="true">
            <path
              fill-rule="evenodd"
              d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z"
              clip-rule="evenodd"
            />
          </svg>
        ) : opt.swatch ? (
          // Project color dot — deterministic from the project name
          // when the backend hasn't shipped a real color yet. P2-22.
          <span
            class="w-2.5 h-2.5 flex-shrink-0 rounded-full ring-1 ring-foreground/10"
            style={{ background: opt.swatch }}
            aria-hidden="true"
          />
        ) : (
          <span class="w-3 flex-shrink-0" aria-hidden="true" />
        )}
        <span class="flex-1 truncate">{opt.label}</span>
      </div>
    </button>
  );
}

/* ═══ Icons ═══ */

function ChevronIcon({ open }: { open: boolean }) {
  // Base glyph points DOWN; when the menu opens upward, rotate so
  // the indicator tracks the menu's actual direction — a user who
  // sees a "v" expects the menu below; when "^" the menu is above.
  return (
    <svg
      class={cn(
        "w-3 h-3 flex-shrink-0 text-muted-foreground transition-transform",
        open && "rotate-180",
      )}
      fill="none"
      viewBox="0 0 24 24"
      stroke="currentColor"
      stroke-width="2.5"
    >
      <path d="M19 9l-7 7-7-7" stroke-linecap="round" stroke-linejoin="round" />
    </svg>
  );
}

function ProjectIcon() {
  return (
    <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.8">
      <path
        stroke-linecap="round"
        stroke-linejoin="round"
        d="M9 17V7m0 10a2 2 0 01-2 2H5a2 2 0 01-2-2V7a2 2 0 012-2h2a2 2 0 012 2m0 10a2 2 0 002 2h2a2 2 0 002-2M9 7a2 2 0 012-2h2a2 2 0 012 2m0 10V7"
      />
    </svg>
  );
}

function TaskIcon() {
  return (
    <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.8">
      <path
        stroke-linecap="round"
        stroke-linejoin="round"
        d="M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2m-6 9l2 2 4-4"
      />
    </svg>
  );
}

function PlayIcon() {
  return (
    <svg class="w-3 h-3 fill-current" viewBox="0 0 24 24">
      <path d="M8 5v14l11-7z" />
    </svg>
  );
}

function MeetingIcon() {
  return (
    <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
      <path stroke-linecap="round" stroke-linejoin="round" d="M15 10l4.553-2.276A1 1 0 0121 8.618v6.764a1 1 0 01-1.447.894L15 14M5 18h8a2 2 0 002-2V8a2 2 0 00-2-2H5a2 2 0 00-2 2v8a2 2 0 002 2z" />
    </svg>
  );
}

function RefreshIcon({ spinning }: { spinning: boolean }) {
  return (
    <svg
      class={cn("w-3.5 h-3.5 transition-transform", spinning && "animate-spin")}
      fill="none"
      viewBox="0 0 24 24"
      stroke="currentColor"
      stroke-width="2"
    >
      <path
        stroke-linecap="round"
        stroke-linejoin="round"
        d="M4 4v5h5M20 20v-5h-5M4.5 15a8 8 0 0014.5 2M19.5 9A8 8 0 005 7"
      />
    </svg>
  );
}
