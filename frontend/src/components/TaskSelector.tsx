import { useState, useEffect, useMemo, useRef } from "preact/hooks";
import type { Task, StartTimerData } from "../app";
import { Button } from "./ui/Button";
import { Input } from "./ui/Input";
import { MarqueeText } from "./ui/MarqueeText";
import { cn } from "../lib/cn";

interface TaskSelectorProps {
  onStart: (data: StartTimerData) => void;
  loading: boolean;
}

export function TaskSelector({ onStart, loading }: TaskSelectorProps) {
  const [tasks, setTasks] = useState<Task[]>([]);
  const [selectedSource, setSelectedSource] = useState("");
  const [selectedTaskId, setSelectedTaskId] = useState("");
  const [description, setDescription] = useState("");
  const [fetchError, setFetchError] = useState("");

  useEffect(() => {
    window.go.main.App.GetMyTasks()
      .then((r) => setTasks(r || []))
      .catch(() => setFetchError("Failed to load tasks"));
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

  function handleStartTask(e: Event) {
    e.preventDefault();
    if (!selectedTask) return;
    // Trim so a whitespace-only description doesn't pass canStartTask
    // (which uses the raw value) and then fail server-side validation
    // with an opaque error. See H-FE-3.
    const trimmed = description.trim();
    if (!trimmed) return;
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
  }

  function handleMeeting() {
    const trimmed = description.trim();
    onStart({
      taskId: "",
      projectId: "",
      taskTitle: "Meeting",
      projectName: "",
      description: trimmed || "Meeting",
    });
    setDescription("");
  }

  // canStartTask uses the trimmed description so the "Start" button
  // correctly disables on whitespace-only input (H-FE-3).
  const canStartTask = description.trim().length > 0 && selectedTaskId;

  return (
    <div class="space-y-2">
      <Input
        type="text"
        placeholder="What are you working on?"
        value={description}
        maxLength={500}
        onInput={(e) => setDescription((e.target as HTMLInputElement).value)}
      />

      <div class="flex gap-1.5">
        <Dropdown
          value={selectedSource}
          placeholder="Select Project"
          icon={<ProjectIcon />}
          options={projects.map((p) => ({ value: p.id, label: p.name }))}
          onChange={(v) => { setSelectedSource(v); setSelectedTaskId(""); }}
        />

        {selectedSource && (
          <Dropdown
            value={selectedTaskId}
            placeholder="Select Task"
            icon={<TaskIcon />}
            options={sourceTasks.map((t) => ({ value: t.taskId, label: t.title }))}
            onChange={setSelectedTaskId}
          />
        )}
      </div>

      {fetchError && (
        <p role="alert" class="text-[10px] text-destructive">
          {fetchError}
        </p>
      )}

      <div class="flex gap-2">
        <Button
          type="button"
          class="flex-1 h-9 font-semibold"
          disabled={loading || !canStartTask}
          onClick={handleStartTask}
        >
          {loading ? (
            <span>…</span>
          ) : (
            <>
              <PlayIcon />
              Start
            </>
          )}
        </Button>
        <Button
          type="button"
          variant="secondary"
          class="flex-1 h-9 font-semibold"
          disabled={loading || !description}
          onClick={handleMeeting}
        >
          <MeetingIcon />
          Meeting
        </Button>
      </div>
    </div>
  );
}

/* ═══ Custom Dropdown ═══ */

interface DropdownOption {
  value: string;
  label: string;
}

function Dropdown({
  value,
  placeholder,
  icon,
  options,
  onChange,
}: {
  value: string;
  placeholder: string;
  icon?: any;
  options: DropdownOption[];
  onChange: (v: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
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

  return (
    <div class="relative flex-1" ref={ref}>
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
        <span class="flex-1 min-w-0">
          {selected ? (
            <MarqueeText>{selected.label}</MarqueeText>
          ) : (
            <span class="truncate">{placeholder}</span>
          )}
        </span>
        <ChevronIcon open={open} />
      </button>

      {open && (
        <div
          role="listbox"
          // The TaskSelector lives in the bottom strip of a 500 px
          // window, so opening downward (mt-1) puts the menu below
          // the footer where it gets visually clipped. Opening
          // UPWARD (bottom-full + mb-1) keeps the whole list inside
          // the viewport regardless of how many projects/tasks the
          // user has. max-h-44 caps the height so a huge list still
          // scrolls internally.
          class="absolute z-50 bottom-full mb-1 w-full rounded-md border border-border bg-popover text-popover-foreground shadow-lg overflow-hidden max-h-44 overflow-y-auto"
        >
          {options.length === 0 ? (
            <div class="px-3 py-2 text-xs text-muted-foreground">No options</div>
          ) : (
            options.map((opt) => (
              <button
                key={opt.value}
                role="option"
                type="button"
                aria-selected={opt.value === value}
                class={cn(
                  "w-full text-left px-3 py-1.5 text-xs transition-colors",
                  "focus-visible:outline-none focus-visible:bg-accent focus-visible:text-accent-foreground",
                  opt.value === value
                    ? "bg-primary/10 text-primary"
                    : "text-foreground hover:bg-accent hover:text-accent-foreground",
                )}
                onClick={() => {
                  onChange(opt.value);
                  setOpen(false);
                }}
              >
                <div class="flex items-center gap-2">
                  {opt.value === value && (
                    <svg class="w-3 h-3 flex-shrink-0" fill="currentColor" viewBox="0 0 20 20">
                      <path
                        fill-rule="evenodd"
                        d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z"
                        clip-rule="evenodd"
                      />
                    </svg>
                  )}
                  <span class="truncate">{opt.label}</span>
                </div>
              </button>
            ))
          )}
        </div>
      )}
    </div>
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
