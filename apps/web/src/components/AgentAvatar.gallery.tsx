import { AgentAvatar } from './AgentAvatar'
import { avatarAccessories, avatarVariants, fallbackAvatar } from './avatar-spec'

/** Development fixture: all 36 combinations, both forms, at small and large sizes. */
export function AgentAvatarGallery({ sizes = [20, 40, 96] }: { sizes?: number[] }) {
  return (
    <div className="grid gap-4">
      {avatarVariants.flatMap((variant) =>
        avatarAccessories.map((accessory) => (
          <div className="flex items-center gap-3" key={`${variant}-${accessory}`}>
            <code>
              {variant} / {accessory}
            </code>
            {sizes.flatMap((size) =>
              (['portrait', 'sprite'] as const).map((form) => (
                <AgentAvatar
                  key={`${form}-${size}`}
                  spec={{ ...fallbackAvatar, variant, accessory }}
                  size={size}
                  form={form}
                  label={`${variant} / ${accessory} / ${form} / ${size}`}
                />
              )),
            )}
          </div>
        )),
      )}
    </div>
  )
}
