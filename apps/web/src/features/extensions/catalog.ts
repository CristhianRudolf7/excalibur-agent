import { Bot, GraduationCap, Microscope, type LucideIcon } from 'lucide-react'

export type ExtensionCategory = 'education' | 'robotics' | 'productivity'
export type ExtensionId = 'classroom' | 'robotlab' | 'robot'

export interface Extension {
  id: ExtensionId
  navigationKey: string
  nameKey: string
  descriptionKey: string
  category: ExtensionCategory
  icon: LucideIcon
  route: '/classroom' | '/lab' | '/robot'
}

/** Built-in catalog. Replace this source when a backend catalog is available. */
export const extensionCatalog: readonly Extension[] = [
  {
    id: 'classroom',
    navigationKey: 'classroom',
    nameKey: 'classroom',
    descriptionKey: 'extensionsClassroomDescription',
    category: 'education',
    icon: GraduationCap,
    route: '/classroom',
  },
  {
    id: 'robotlab',
    navigationKey: 'lab_title',
    nameKey: 'lab_title',
    descriptionKey: 'lab_description',
    category: 'robotics',
    icon: Microscope,
    route: '/lab',
  },
  {
    id: 'robot',
    navigationKey: 'robot',
    nameKey: 'extensionsRobotArm',
    descriptionKey: 'extensionsRobotDescription',
    category: 'robotics',
    icon: Bot,
    route: '/robot',
  },
]

export const extensionCategories: readonly ExtensionCategory[] = [
  'education',
  'robotics',
  'productivity',
]
