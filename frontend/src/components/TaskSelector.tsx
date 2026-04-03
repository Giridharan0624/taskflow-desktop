import { useState, useEffect, useMemo } from "preact/hooks";
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
    onStart({
      taskId: selectedTask.taskId,
      projectId: selectedTask.projectId,
      taskTitle: selectedTask.title,
      projectName: selectedTask.projectName || "",
      description,
    });
    setDescription("");
    setSelectedSource("");
    setSelectedTaskId("");
  }

  function handleMeeting() {
    onStart({
      taskId: "",
      projectId: "",
      taskTitle: "Meeting",
      projectName: "",
      description: description || "Meeting",
    });
    setDescription("");
  }

  const canStartTask = description && selectedTaskId;

  return (
    <div class="space-y-2">
      {/* Description */}
      <input
        type="text"
        class="input"
        placeholder="What are you working on?"
        value={description}
        onInput={(e) => setDescription((e.target as HTMLInputElement).value)}
      />

      {/* Source + Task row */}
      <div class="flex gap-1.5">
        <select
          class="input !py-1.5 !text-[12px] flex-1"
          value={selectedSource}
          onChange={(e) => {
            setSelectedSource((e.target as HTMLSelectElement).value);
            setSelectedTaskId("");
          }}
        >
          <option value="">Source</option>
          {projects.map((p) => (
            <option value={p.id} key={p.id}>
              {p.name}
            </option>
          ))}
        </select>

        {selectedSource && (
          <select
            class="input !py-1.5 !text-[12px] flex-1"
            value={selectedTaskId}
            onChange={(e) =>
              setSelectedTaskId((e.target as HTMLSelectElement).value)
            }
          >
            <option value="">Task</option>
            {sourceTasks.map((t) => (
              <option value={t.taskId} key={t.taskId}>
                {t.title}
              </option>
            ))}
          </select>
        )}
      </div>

      {fetchError && <p class="text-red-400 text-[10px]">{fetchError}</p>}

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
