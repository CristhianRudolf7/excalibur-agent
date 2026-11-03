import { FilterSelect } from '@/features/settings/FilterSelect'
import { parameterFields } from './logic'
import type { JSONSchema, Params, RewardTerm } from './types'
import { useLabText } from './i18n'
export function ParamsForm({
  schema,
  value,
  onChange,
}: {
  schema: JSONSchema
  value: Params
  onChange: (params: Params) => void
}) {
  return (
    <div className="lab-fields">
      {parameterFields(schema).map((field) => {
        const current = value[field.name] ?? field.default ?? ''
        const update = (next: unknown) => onChange({ ...value, [field.name]: next })
        const title = field.title ?? field.name
        return (
          <label key={field.name}>
            <span>{title}</span>
            {field.enum ? (
              <FilterSelect
                label={title}
                value={String(current)}
                options={field.enum.map((option) => ({
                  value: String(option),
                  label: String(option),
                }))}
                onChange={(next) => update(field.enum!.find((item) => String(item) === next))}
              />
            ) : field.type === 'boolean' ? (
              <input
                type="checkbox"
                checked={Boolean(current)}
                onChange={(event) => update(event.target.checked)}
              />
            ) : (
              <input
                aria-label={title}
                required={field.required}
                type={field.type === 'number' || field.type === 'integer' ? 'number' : 'text'}
                min={field.minimum}
                max={field.maximum}
                step={field.type === 'integer' ? 1 : 'any'}
                value={String(current)}
                onChange={(event) =>
                  update(
                    field.type === 'number' || field.type === 'integer'
                      ? event.target.valueAsNumber
                      : event.target.value,
                  )
                }
              />
            )}
          </label>
        )
      })}
    </div>
  )
}
export function RewardWeights({
  terms,
  value,
  onChange,
}: {
  terms: RewardTerm[]
  value: Record<string, number>
  onChange: (weights: Record<string, number>) => void
}) {
  const { text, language } = useLabText()
  return (
    <fieldset className="lab-fields">
      <legend>{text('rewards')}</legend>
      {terms.map((term) => (
        <label title={term.description[language]} key={term.name}>
          <span>{term.name}</span>
          <input
            aria-label={term.name}
            type="number"
            step="any"
            value={value[term.name] ?? term.weight}
            onChange={(event) => {
              if (Number.isFinite(event.target.valueAsNumber))
                onChange({ ...value, [term.name]: event.target.valueAsNumber })
            }}
          />
        </label>
      ))}
    </fieldset>
  )
}
