import {
  BookOpen,
  Briefcase,
  Calendar,
  Database,
  GraduationCap,
  Heart,
  List,
  Wallet,
} from 'lucide-react'
/** Recognize common agent-provided icon names and emoji without requiring emoji fonts. */
export function CollectionIcon({ name, size = 20 }: { name?: string; size?: number }) {
  const icons = {
    database: Database,
    'graduation-cap': GraduationCap,
    graduationcap: GraduationCap,
    '🎓': GraduationCap,
    book: BookOpen,
    'book-open': BookOpen,
    '📚': BookOpen,
    wallet: Wallet,
    '💰': Wallet,
    calendar: Calendar,
    '📅': Calendar,
    heart: Heart,
    '❤️': Heart,
    briefcase: Briefcase,
    '💼': Briefcase,
    list: List,
  }
  const Icon = icons[(name ?? '').toLowerCase() as keyof typeof icons] ?? Database
  return <Icon size={size} aria-hidden="true" />
}
