import type { AttendanceSession } from "../app";
import { Timer, formatDuration } from "./Timer";

interface SessionListProps {
  sessions: AttendanceSession[];
  currentSignInAt: string | null;
  onResume?: (session: AttendanceSession) => void;
}

/**
 * SessionList — displays today's work sessions with durations.
 */
export function SessionList({
  sessions,
  currentSignInAt,
  onResume,
}: SessionListProps) {
  if (!sessions || sessions.length === 0) {
    return (
      <div class="text-center text-gray-400 text-xs py-4">
        No sessions today
      </div>
    );
  }

  const grouped = groupSessionsByTask(sessions);

  return (
    <div class="space-y-1">
      <h3 class="text-xs font-medium text-gray-500 uppercase tracking-wide mb-2">
        Today's Sessions
      </h3>
      {grouped.map((group, i) => (
        <div key={i} class="session-item">
          <div class="flex-1 min-w-0">
            <p class="font-medium text-gray-900 truncate text-sm">
              {group.taskTitle || group.description || "Meeting"}
            </p>
            {group.projectName && (
              <p class="text-gray-500 text-xs truncate">{group.projectName}</p>
            )}
          </div>

          <div class="flex items-center gap-2 ml-2">
            {group.isActive && currentSignInAt ? (
              <Timer
                startTime={currentSignInAt}
                class="text-emerald-600 font-mono text-sm font-medium"
              />
            ) : (
              <span class="text-gray-600 font-mono text-sm">
                {formatDuration(group.totalHours)}
              </span>
            )}

            {!group.isActive && onResume && (
              <button
                class="text-primary-600 hover:text-primary-700 text-xs font-medium px-2 py-1 rounded hover:bg-primary-50"
                onClick={() =>
                  // Resume the MOST RECENT session for this task — not
                  // the first one in the list, which is the oldest and
                  // often has outdated task metadata (old description,
                  // pre-rename project name, etc.). See H-FE-4.
                  onResume(group.sessions[group.sessions.length - 1])
                }
                title="Resume this task"
              >
                Resume
              </button>
            )}
          </div>
        </div>
      ))}
    </div>
  );
}

interface GroupedSession {
  taskId: string | null;
  taskTitle: string | null;
  projectName: string | null;
  description: string | null;
  totalHours: number;
  isActive: boolean;
  sessions: AttendanceSession[];
}

function groupSessionsByTask(
  sessions: AttendanceSession[]
): GroupedSession[] {
  const map = new Map<string, GroupedSession>();

  for (const session of sessions) {
    const key = session.taskId || session.description || "meeting";
    const existing = map.get(key);
    const isActive = !session.signOutAt;

    let hours = 0;
    if (!isActive) {
      if (session.hours && session.hours > 0) {
        hours = session.hours;
      } else if (session.signOutAt && session.signInAt) {
        hours =
          (new Date(session.signOutAt).getTime() -
            new Date(session.signInAt).getTime()) /
          3600000;
      }
    }

    if (existing) {
      existing.totalHours += hours;
      existing.isActive = existing.isActive || isActive;
      existing.sessions.push(session);
    } else {
      map.set(key, {
        taskId: session.taskId,
        taskTitle: session.taskTitle,
        projectName: session.projectName,
        description: session.description,
        totalHours: hours,
        isActive,
        sessions: [session],
      });
    }
  }

  return Array.from(map.values()).sort((a, b) => {
    if (a.isActive && !b.isActive) return -1;
    if (!a.isActive && b.isActive) return 1;
    return b.totalHours - a.totalHours;
  });
}
