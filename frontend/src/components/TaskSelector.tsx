import { useState, useEffect, useMemo, useRef } from "preact/hooks";
import type { Task, StartTimerData } from "../app";

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
      {/* Description */}
      <input
        type="text"
        class="input"
        placeholder="What are you working on?"
        value={description}
        maxLength={500}
        onInput={(e) => setDescription((e.target as HTMLInputElement).value)}
      />

      {/* Project + Task dropdowns */}
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

      {fetchError && <p class="text-[10px]" style={{ color: "var(--color-danger, #ef4444)" }}>{fetchError}</p>}

      {/* Buttons */}
      <div class="flex gap-1.5">
        <button
          type="button"
          class="btn-primary flex-1 !py-1.5"
          disabled={loading || !canStartTask}
          onClick={handleStartTask}
        >
          {loading ? "..." : "Start"}
        </button>
        <button
          type="button"
          class="btn-meeting flex-1 !py-1.5"
          disabled={loading || !description}
          onClick={handleMeeting}
        >
          Meeting
        </button>
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
      {/* Trigger */}
      <button
        type="button"
        class="w-full flex items-center gap-1.5 px-2.5 py-1.5 rounded-xl text-[12px] text-left transition-all"
        style={{
          background: "var(--color-surface)",
          border: open ? "1px solid var(--color-primary)" : "1px solid var(--color-border)",
          boxShadow: open ? "0 0 0 3px rgba(99, 102, 241, 0.12)" : "none",
          color: selected ? "var(--color-text)" : "var(--color-text-muted)",
        }}
        onClick={() => setOpen(!open)}
      >
        {icon && <span style={{ color: "var(--color-text-muted)", flexShrink: 0 }}>{icon}</span>}
        <span class="flex-1 truncate">{selected ? selected.label : placeholder}</span>
        <ChevronIcon open={open} />
      </button>

      {/* Dropdown list */}
      {open && (
        <div
          class="absolute z-50 mt-1 w-full rounded-xl overflow-hidden"
          style={{
            background: "var(--color-surface)",
            border: "1px solid var(--color-border)",
            boxShadow: "0 8px 24px rgba(0,0,0,0.12)",
            maxHeight: 180,
            overflowY: "auto",
          }}
        >
          {options.length === 0 ? (
            <div class="px-3 py-2 text-[11px]" style={{ color: "var(--color-text-muted)" }}>
              No options
            </div>
          ) : (
            options.map((opt) => (
              <button
                key={opt.value}
                type="button"
                class="w-full text-left px-3 py-2 text-[12px] transition-colors"
                style={{
                  color: opt.value === value ? "var(--color-primary)" : "var(--color-text)",
                  background: opt.value === value ? "var(--color-primary-light)" : "transparent",
                }}
                onMouseEnter={(e) => {
                  if (opt.value !== value) (e.currentTarget as HTMLElement).style.background = "var(--color-surface-hover)";
                }}
                onMouseLeave={(e) => {
                  (e.currentTarget as HTMLElement).style.background = opt.value === value ? "var(--color-primary-light)" : "transparent";
                }}
                onClick={() => { onChange(opt.value); setOpen(false); }}
              >
                <div class="flex items-center gap-2">
                  {opt.value === value && (
                    <svg class="w-3 h-3 flex-shrink-0" fill="currentColor" viewBox="0 0 20 20">
                      <path fill-rule="evenodd" d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z" clip-rule="evenodd" />
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
  return (
    <svg
      class="w-3 h-3 flex-shrink-0 transition-transform"
      style={{ transform: open ? "rotate(180deg)" : "rotate(0)", color: "var(--color-text-muted)" }}
      fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"
    >
      <path d="M19 9l-7 7-7-7" stroke-linecap="round" stroke-linejoin="round" />
    </svg>
  );
}

function ProjectIcon() {
  return (
    <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.8">
      <path stroke-linecap="round" stroke-linejoin="round" d="M9 17V7m0 10a2 2 0 01-2 2H5a2 2 0 01-2-2V7a2 2 0 012-2h2a2 2 0 012 2m0 10a2 2 0 002 2h2a2 2 0 002-2M9 7a2 2 0 012-2h2a2 2 0 012 2m0 10V7" />
    </svg>
  );
}

function TaskIcon() {
  return (
    <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.8">
      <path stroke-linecap="round" stroke-linejoin="round" d="M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2m-6 9l2 2 4-4" />
    </svg>
  );
}
