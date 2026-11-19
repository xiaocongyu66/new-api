export type ReferralDescriptionInput = {
  mode: 'direct' | 'recharge'
  trigger: 'first' | 'every'
  multiplierPercent: string
  inviterReward: number
  inviteeReward: number
  inviterRewardLabel: string
  inviteeRewardLabel: string
  minTransfer: number
  minTransferLabel: string
}

type Translate = (key: string, values?: Record<string, string>) => string

/**
 * Build the referral card description for every combination of reward mode,
 * configured amounts, rebate trigger and transfer threshold.
 */
export function buildReferralDescription(
  t: Translate,
  input: ReferralDescriptionInput
): string {
  const hasInviter = input.inviterReward > 0
  const hasInvitee = input.inviteeReward > 0

  const rewardSentence =
    input.mode === 'recharge'
      ? input.trigger === 'first'
        ? t(
            "New users who sign up through your link earn you {{percent}} back after the invitee's first top-up.",
            { percent: input.multiplierPercent }
          )
        : t(
            'New users who sign up through your link earn you {{percent}} back after every invitee top-up.',
            { percent: input.multiplierPercent }
          )
      : hasInviter && hasInvitee
        ? t('New users who sign up through your link earn you {{inviter}} and your friend {{invitee}}.', {
            inviter: input.inviterRewardLabel,
            invitee: input.inviteeRewardLabel,
          })
        : hasInviter
          ? t('New users who sign up through your link earn you {{inviter}}.', {
              inviter: input.inviterRewardLabel,
            })
          : hasInvitee
            ? t('New users who sign up through your link earn your friend {{invitee}}.', {
                invitee: input.inviteeRewardLabel,
              })
            : t('Share your link with friends.')


  // With no configured reward there is nothing to accumulate or transfer.
  if (input.mode === 'direct' && !hasInviter && !hasInvitee) {
    return rewardSentence
  }


  const transferSentence =
    input.mode === 'recharge'
      ? input.minTransfer > 0
        ? t('Recharge rebates can be transferred once you reach {{amount}}.', {
            amount: input.minTransferLabel,
          })
        : t('Recharge rebates can be transferred to your balance anytime.')
      : input.minTransfer > 0
        ? t('Transfer rewards to your balance once you reach {{amount}}.', {
            amount: input.minTransferLabel,
          })
        : t('Transfer rewards to your balance anytime.')

  const separator = /[。！？]$/.test(rewardSentence) ? '' : ' '
  return `${rewardSentence}${separator}${transferSentence}`
}
