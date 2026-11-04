import { useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Markdown } from '@/components/markdown/Markdown'
import { Chat } from '@/features/chat/Chat'
import { labCall } from './api'
import { Tabs } from './Tabs'
import { useLabText, type LabMessage } from './i18n'
import { ActivityImage } from './ActivityImage'
import { validLogImage } from './logic'
import { useInitialChatScroll } from './useInitialChatScroll'
import { useSessionLog } from './useSessionLog'
import { latestRobotState, robotState } from './robotState'
import { RobotState } from './RobotState'
import type { Session } from './types'

export function SessionAgent({
  session,
  attempt,
  time,
}: {
  session: Session
  attempt: number
  time?: number
}) {
  const { text } = useLabText()
  const log = useSessionLog(session, attempt)
  const snapshot = latestRobotState(log.data?.entries ?? [], session.id, attempt, time)
  const current = useQuery({
    queryKey: ['robotlab', 'robot-state', session.id, attempt],
    queryFn: ({ signal }) => labCall('robotlab.robot.state', { session: session.id }, { signal }),
    enabled: session.status === 'live' && !snapshot,
    refetchInterval: 2000,
    retry: false,
  })
  const state =
    snapshot?.state ??
    (session.status === 'live' && time === undefined ? robotState(current.data) : undefined)
  const [tab, setTab] = useState<'chat' | 'activity' | 'skills'>('chat')
  const chatPanel = useRef<HTMLDivElement>(null)
  useInitialChatScroll(chatPanel, tab === 'chat', session.id)
  return (
    <>
      <RobotState robot={session.robot} state={state} time={snapshot?.time} />
      <Tabs
        label={text('agent')}
        values={['chat', 'activity', 'skills']}
        value={tab}
        onChange={setTab}
      />
      <div ref={chatPanel} className="lab-agent-panel" hidden={tab !== 'chat'}>
        <Chat agentId="excalibur" onFile={() => {}} />
      </div>
      {tab === 'activity' && (
        <ActivityContent session={session} attempt={attempt} time={time} log={log} />
      )}
      {tab === 'skills' && <Skills session={session.id} />}
    </>
  )
}
export function Activity({
  session,
  attempt,
  time,
}: {
  session: Session
  attempt: number
  time?: number
}) {
  const log = useSessionLog(session, attempt)
  return <ActivityContent session={session} attempt={attempt} time={time} log={log} />
}
function ActivityContent({
  session,
  attempt,
  time,
  log,
}: {
  session: Session
  attempt: number
  time?: number
  log: ReturnType<typeof useSessionLog>
}) {
  const { text, language } = useLabText()
  const entries =
    log.data?.entries.filter((entry) => time === undefined || entry.sim_time <= time) ?? []
  const skillEvents = session.skill_events.filter((event) => event.attempt === attempt)
  return (
    <div className="lab-agent-panel">
      {log.isPending && <p role="status">{text('loading')}</p>}
      {log.error && <p role="alert">{log.error.message}</p>}
      {!log.isPending && entries.length === 0 && skillEvents.length === 0 && (
        <p>{text('noActivity')}</p>
      )}
      <ol className="lab-log">
        {entries.map((entry) => (
          <li key={entry.id}>
            <strong>{text(entry.kind)}</strong>
            <time dateTime={entry.t}>
              {new Date(entry.t).toLocaleTimeString(language)} · {entry.sim_time.toFixed(2)} s
            </time>
            <p>{entry.summary}</p>
            {entry.image && validLogImage(entry.image) && (
              <ActivityImage session={session.id} path={entry.image} />
            )}
          </li>
        ))}
        {skillEvents.map((event, index) => (
          <li key={`skill-${index}`}>
            <strong>{text('skill')}</strong>
            <p>
              {event.skill} · {text(event.kind as LabMessage)} · {text('version')} {event.version}
            </p>
          </li>
        ))}
      </ol>
    </div>
  )
}
export function Skills({ session }: { session: string }) {
  const { text, language } = useLabText()
  const skills = useQuery({
    queryKey: ['robotlab', 'skills', session],
    queryFn: () => labCall('robotlab.skills.list', { session }),
    refetchInterval: 5000,
  })
  return (
    <div className="lab-agent-panel">
      {skills.isPending && <p role="status">{text('loading')}</p>}
      {skills.error && <p role="alert">{skills.error.message}</p>}
      {skills.data?.skills.length === 0 && <p>{text('noSkills')}</p>}
      <ul className="lab-skills">
        {skills.data?.skills.map((skill) => (
          <li key={skill.name}>
            <p>{skill.description}</p>
            <p>
              {text('version')}: {skill.version} · {text('success')}:{' '}
              {skill.success_rate === null
                ? text('unknown')
                : new Intl.NumberFormat(language, {
                    style: 'percent',
                    maximumFractionDigits: 0,
                  }).format(skill.success_rate)}{' '}
              · {text('uses')}: {skill.uses} · {text('failures')}: {skill.failures}
            </p>
            <SkillDetails session={session} name={skill.name} />
          </li>
        ))}
      </ul>
    </div>
  )
}

function SkillDetails({ session, name }: { session: string; name: string }) {
  const { text, language } = useLabText()
  const [open, setOpen] = useState(false)
  const detail = useQuery({
    queryKey: ['robotlab', 'skill', session, name],
    queryFn: () => labCall('robotlab.skills.read', { session, name }),
    enabled: open,
    refetchInterval: open ? 5000 : false,
  })
  return (
    <details onToggle={(event) => setOpen(event.currentTarget.open)}>
      <summary>{name}</summary>
      {open && (
        <>
          {detail.isPending && <p role="status">{text('loading')}</p>}
          {detail.error && <p role="alert">{detail.error.message}</p>}
          {detail.data && (
            <>
              <Markdown>{detail.data.skill.instructions}</Markdown>
              <h3>{text('versions')}</h3>
              <ol aria-label={text('versions')}>
                {detail.data.skill.versions.map((version) => (
                  <li key={version.version}>
                    {text('version')} {version.version} ·{' '}
                    <time dateTime={version.created_at}>
                      {new Date(version.created_at).toLocaleString(language)}
                    </time>
                  </li>
                ))}
              </ol>
            </>
          )}
        </>
      )}
    </details>
  )
}
