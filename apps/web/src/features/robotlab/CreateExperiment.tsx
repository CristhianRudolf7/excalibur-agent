import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { FilterSelect } from '@/features/settings/FilterSelect'
import type { Experiment, Params, ScenarioInfo } from './types'
import { labCall } from './api'
import { ParamsForm, RewardWeights } from './ParamsForm'
import { useLabText } from './i18n'
export function CreateExperiment({
  scenario,
  weights: initialWeights,
  onClose,
  onCreated,
}: {
  scenario: ScenarioInfo
  weights: Record<string, number>
  onClose: () => void
  onCreated: (experiment: Experiment) => void
}) {
  const { text } = useLabText()
  const [name, setName] = useState('')
  const [params, setParams] = useState<Params>(scenario.default_params)
  const [weights, setWeights] = useState(initialWeights)
  const [steps, setSteps] = useState(scenario.default_total_steps)
  const [runs, setRuns] = useState([
    { label: `${text('run')} 1`, algorithm: scenario.algorithms[0] ?? 'PPO', hyperparams: '{}' },
  ])
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  return (
    <Dialog title={text('new')} onClose={onClose} className="lab-dialog">
      <form
        className="lab-fields"
        onSubmit={(event) => {
          event.preventDefault()
          setError('')
          try {
            const parsed = runs.map((run) => {
              const hyperparams: unknown = JSON.parse(run.hyperparams)
              if (!hyperparams || typeof hyperparams !== 'object' || Array.isArray(hyperparams))
                throw new Error(text('hyperparams'))
              return { ...run, hyperparams: hyperparams as Params }
            })
            setBusy(true)
            void labCall('robotlab.experiment.create', {
              name,
              scenario: scenario.id,
              params,
              runs: parsed,
              total_steps: steps,
              reward_weights: weights,
            })
              .then(onCreated)
              .catch((cause: unknown) => setError(String(cause)))
              .finally(() => setBusy(false))
          } catch (cause) {
            setError(String(cause))
          }
        }}
      >
        <label>
          {text('name')}
          <input
            aria-label={text('name')}
            required
            maxLength={160}
            value={name}
            onChange={(event) => setName(event.target.value)}
          />
        </label>
        <ParamsForm schema={scenario.params_schema} value={params} onChange={setParams} />
        <label>
          {text('steps')}
          <input
            aria-label={text('steps')}
            type="number"
            min={1}
            max={100000000}
            required
            value={steps}
            onChange={(event) => setSteps(event.target.valueAsNumber)}
          />
        </label>
        {runs.map((run, index) => (
          <fieldset key={index}>
            <legend>{run.label}</legend>
            <label>
              {text('name')}
              <input
                value={run.label}
                required
                aria-label={`${text('name')} ${index + 1}`}
                onChange={(event) =>
                  setRuns(
                    runs.map((item, i) =>
                      i === index ? { ...item, label: event.target.value } : item,
                    ),
                  )
                }
              />
            </label>
            <FilterSelect
              label={`${text('algorithm')} ${index + 1}`}
              value={run.algorithm}
              options={scenario.algorithms.map((algorithm) => ({
                value: algorithm,
                label: algorithm,
              }))}
              onChange={(algorithm) =>
                setRuns(runs.map((item, i) => (i === index ? { ...item, algorithm } : item)))
              }
            />
            <label>
              {text('hyperparams')}
              <textarea
                aria-label={`${text('hyperparams')} ${index + 1}`}
                value={run.hyperparams}
                onChange={(event) =>
                  setRuns(
                    runs.map((item, i) =>
                      i === index ? { ...item, hyperparams: event.target.value } : item,
                    ),
                  )
                }
              />
            </label>
            {runs.length > 1 && (
              <Button onClick={() => setRuns(runs.filter((_, i) => i !== index))}>
                {text('remove')}
              </Button>
            )}
          </fieldset>
        ))}
        <Button
          disabled={runs.length >= 4}
          onClick={() =>
            setRuns([
              ...runs,
              {
                label: `${text('run')} ${runs.length + 1}`,
                algorithm: scenario.algorithms[runs.length % scenario.algorithms.length] ?? 'PPO',
                hyperparams: '{}',
              },
            ])
          }
        >
          {text('addRun')}
        </Button>
        <RewardWeights terms={scenario.reward_terms} value={weights} onChange={setWeights} />
        {error && <p role="alert">{error}</p>}
        <Button variant="primary" type="submit" disabled={busy}>
          {text('create')}
        </Button>
      </form>
    </Dialog>
  )
}
