import { create } from 'zustand'
import type { Status } from '@/components/shared'
import { getTasks, type Task } from './mock-data'
export const statuses: Status[] = ['pending', 'progress', 'review', 'done', 'blocked', 'cancelled']
export const useTasks = create<{
  tasks: Task[]
  cycle: (id: string) => void
  complete: (id: string) => void
}>((set) => ({
  tasks: getTasks(),
  cycle: (id) =>
    set((s) => ({
      tasks: s.tasks.map((task) =>
        task.id === id
          ? { ...task, status: statuses[(statuses.indexOf(task.status) + 1) % statuses.length]! }
          : task,
      ),
    })),
  complete: (id) =>
    set((s) => ({
      tasks: s.tasks.map((task) => (task.id === id ? { ...task, status: 'done' } : task)),
    })),
}))
