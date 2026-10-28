import type { Schemas } from '@myfantasyprem/api-client';
import { useLocalSearchParams, useRouter } from 'expo-router';
import { useState } from 'react';
import { View } from 'react-native';

import { useLeagueContext, useTeam } from '@/api/hooks';
import { useProposeTrade } from '@/api/moves-hooks';
import { AppText } from '@/components/AppText';
import { Avatar } from '@/components/Badges';
import { Button, IconButton } from '@/components/Button';
import { Card } from '@/components/Card';
import { EmptyState } from '@/components/EmptyState';
import { ArrowLeftRight, Check, CircleAlert, Plus } from '@/components/icons';
import { ListRow } from '@/components/ListRow';
import { PlayerName, usePlayerCard } from '@/components/players/PlayerCardContext';
import { PlayerRow } from '@/components/PlayerRow';
import { QueryStatus } from '@/components/QueryStatus';
import { Screen } from '@/components/Screen';
import { SectionHeader } from '@/components/SectionHeader';
import { TextField } from '@/components/TextField';
import { useToast } from '@/components/Toast';
import { useLayout } from '@/hooks/useLayout';
import { describeError, moveErrorText, problemErrors } from '@/lib/errors';
import { ordinal } from '@/lib/leagues';
import { parseOrigin, withOrigin } from '@/lib/navigation';
import { MAX_TRADE_MESSAGE, checkTrade, positionSummary, togglePlayer } from '@/lib/trade';
import {
  clearTradeDraft,
  isRefused,
  reviseDraft,
  saveTradeDraft,
  tradeDraft,
} from '@/lib/trade-draft';
import { useTheme } from '@/theme';

export { RouteError as ErrorBoundary } from '@/components/RouteError';

type Player = Schemas['PlayerRef'];
type Step = 'team' | 'players' | 'review';

/**
 * Propose a trade: pick the team, pick players on both sides (positions
 * must match, so both squads stay legal), review with an optional
 * message, send. A player card or team page can start it with the team
 * and a player already chosen; a refused offer's "Edit offer" reopens it
 * (`?draft=1`) with what is still valid, saying which players went and why.
 */
export default function NewTradeScreen() {
  const params = useLocalSearchParams<{
    leagueId: string;
    teamId?: string;
    playerId?: string;
    from?: string;
    draft?: string;
  }>();
  const from = parseOrigin(params.from);
  const { leagueId } = params;
  const router = useRouter();
  const toast = useToast();
  const { spacing, colors } = useTheme();
  const { isWide } = useLayout();
  const home = useLeagueContext(leagueId);
  const h = home.data?.data;
  const myTeam = h?.viewer.team ?? null;
  // The refused offer this builder reopens, if any (read once).
  const [refused] = useState(() => {
    const d = params.draft ? tradeDraft(leagueId) : undefined;
    return isRefused(d) ? d : undefined;
  });
  const [teamId, setTeamId] = useState<string | undefined>(refused?.teamId ?? params.teamId);
  const [step, setStep] = useState<Step>(
    refused ? (refused.removed.length ? 'players' : 'review') : params.teamId ? 'players' : 'team',
  );
  const [give, setGive] = useState<Player[]>(refused?.give ?? []);
  const [get, setGet] = useState<Player[] | null>(refused?.get ?? null);
  const [message, setMessage] = useState(refused?.message ?? '');
  const mine = useTeam(leagueId, myTeam?.id ?? '');
  const theirs = useTeam(leagueId, teamId ?? '');
  const propose = useProposeTrade(leagueId);
  const openCard = usePlayerCard();

  // Preselect the player the card asked about, once their roster loads.
  const getList =
    get ??
    (params.playerId && theirs.data
      ? theirs.data.data.roster.filter((r) => r.player.id === params.playerId).map((r) => r.player)
      : []);
  const check = checkTrade(give, getList, message);
  const deadline = h?.settings.trades.deadlineGameweek ?? null;
  const pastDeadline = h && deadline !== null && h.currentGameweek.number > deadline;
  const other = h?.standings.find((r) => r.team.id === teamId)?.team ?? theirs.data?.data.team;

  // Optimistic: the offer heads the outgoing list at once; a refusal takes
  // it out again and the toast gives the server's reasons, with "Edit offer"
  // back to this builder holding the offer (less the players ruled out).
  const send = () => {
    const draft = {
      teamId: teamId!,
      teamName: other?.name,
      give,
      get: getList,
      message,
    };
    saveTradeDraft(leagueId, draft);
    propose
      .mutateAsync({
        receivingTeamId: teamId!,
        offeredPlayerIds: give.map((p) => p.id),
        requestedPlayerIds: getList.map((p) => p.id),
        ...(message.trim() ? { message: message.trim() } : {}),
      })
      .then(
        () => {
          clearTradeDraft(leagueId);
          toast({
            message: `Offer sent to ${other?.name ?? 'the other team'}`,
            tone: 'success',
            haptic: false,
          });
        },
        (e: unknown) => {
          saveTradeDraft(leagueId, reviseDraft(draft, e));
          toast({
            message: moveErrorText(e),
            tone: 'error',
            haptic: false,
            action: {
              label: 'Edit offer',
              onPress: () =>
                router.push(withOrigin(`/leagues/${leagueId}/players/trades/new?draft=1`, from)),
            },
          });
        },
      );
    router.replace(withOrigin(`/leagues/${leagueId}/players/trades?tab=outgoing`, from));
  };

  const pickList = (
    title: string,
    query: ReturnType<typeof useTeam>,
    chosen: Player[],
    onToggle: (p: Player) => void,
    testID: string,
  ) => (
    <View style={{ flex: 1, minWidth: 0, gap: spacing.xs }}>
      <SectionHeader title={title} detail={chosen.length ? positionSummary(chosen) : undefined} />
      <QueryStatus query={query}>
        {({ data }) => (
          <Card flush testID={testID}>
            {data.roster.map((r, i) => {
              const selected = chosen.some((p) => p.id === r.player.id);
              return (
                <PlayerRow
                  key={r.player.id}
                  testID={`${testID}-${r.player.id}`}
                  player={r.player}
                  divider={i > 0}
                  points={r.totalPoints}
                  pointsLabel="pts"
                  selected={selected}
                  // The row opens the card; the trade toggle is its own button beside it.
                  onOpenCard={openCard ? () => openCard(r.player.id) : undefined}
                  trailing={
                    <IconButton
                      icon={selected ? Check : Plus}
                      label={
                        selected
                          ? `Remove ${r.player.displayName} from the trade`
                          : `Add ${r.player.displayName} to the trade`
                      }
                      selected={selected}
                      color={selected ? colors.accent : colors.textMuted}
                      onPress={() => onToggle(r.player)}
                      testID={`${testID}-toggle-${r.player.id}`}
                    />
                  }
                />
              );
            })}
          </Card>
        )}
      </QueryStatus>
    </View>
  );

  let body;
  if (home.data && !myTeam) {
    body = (
      <Card>
        <EmptyState icon={ArrowLeftRight} title="You need a team to trade" />
      </Card>
    );
  } else if (pastDeadline) {
    body = (
      <Card>
        <EmptyState
          icon={ArrowLeftRight}
          title="The trade deadline has passed"
          body={`Trades closed after gameweek ${deadline}.`}
          testID="trade-deadline"
        />
      </Card>
    );
  } else if (step === 'team') {
    body = (
      <View style={{ gap: spacing.xs }}>
        <SectionHeader title="Trade with" />
        <QueryStatus query={home}>
          {({ data }) => (
            <Card flush testID="trade-teams">
              {data.standings
                .filter((r) => r.team.id !== myTeam?.id)
                .map((r, i) => (
                  <ListRow
                    key={r.team.id}
                    divider={i > 0}
                    left={<Avatar name={r.team.name} size={32} square />}
                    title={r.team.name}
                    subtitle={`${r.team.managerName} · ${ordinal(r.rank)}`}
                    onPress={() => {
                      setTeamId(r.team.id);
                      setGet([]);
                      setStep('players');
                    }}
                    chevron
                    testID={`trade-team-${r.team.id}`}
                  />
                ))}
            </Card>
          )}
        </QueryStatus>
      </View>
    );
  } else if (step === 'players') {
    body = (
      <>
        <View
          style={{
            flexDirection: isWide ? 'row' : 'column',
            alignItems: isWide ? 'flex-start' : 'stretch',
            gap: spacing.lg,
          }}
        >
          {pickList('You give', mine, give, (p) => setGive((g) => togglePlayer(g, p)), 'give')}
          {pickList(
            `You get from ${other?.name ?? 'them'}`,
            theirs,
            getList,
            (p) => setGet(togglePlayer(getList, p)),
            'get',
          )}
        </View>
      </>
    );
  } else {
    body = (
      <View style={{ gap: spacing.md }} testID="trade-review-step">
        <Card style={{ gap: spacing.md }}>
          <AppText variant="heading">{`Offer to ${other?.name ?? 'them'}`}</AppText>
          <View style={{ flexDirection: isWide ? 'row' : 'column', gap: spacing.md }}>
            <View style={{ flex: 1, gap: spacing.xs, alignItems: 'flex-start' }}>
              <AppText variant="eyebrow" muted>
                You give
              </AppText>
              {give.map((p) => (
                <PlayerName key={p.id} player={p} label={`${p.displayName} · ${p.club.code}`} />
              ))}
            </View>
            <View style={{ flex: 1, gap: spacing.xs, alignItems: 'flex-start' }}>
              <AppText variant="eyebrow" muted>
                You get
              </AppText>
              {getList.map((p) => (
                <PlayerName key={p.id} player={p} label={`${p.displayName} · ${p.club.code}`} />
              ))}
            </View>
          </View>
          <AppText variant="meta" muted>
            Unanswered offers expire at the next waiver run.
          </AppText>
        </Card>
        <TextField
          label="Message (optional)"
          value={message}
          onChangeText={setMessage}
          maxLength={MAX_TRADE_MESSAGE}
          multiline
          hint={`${message.length}/${MAX_TRADE_MESSAGE}`}
          testID="trade-message"
        />
        {propose.isError ? (
          <Card testID="trade-error" style={{ borderColor: colors.loss }}>
            <AppText weight="semibold" role="alert">
              {describeError(propose.error)}
            </AppText>
            {problemErrors(propose.error).map((m) => (
              <AppText key={m} variant="meta" muted>{`• ${m}`}</AppText>
            ))}
          </Card>
        ) : null}
      </View>
    );
  }

  const footer =
    step === 'players' && !pastDeadline ? (
      <Card style={{ gap: spacing.sm }} testID="trade-check">
        {check.ok ? (
          <AppText variant="meta" color={colors.win}>
            {`${positionSummary(give)} for ${positionSummary(getList)}: a legal trade.`}
          </AppText>
        ) : (
          check.problems.map((p) => (
            <View
              key={p}
              style={{ flexDirection: 'row', gap: spacing.sm, alignItems: 'flex-start' }}
            >
              <CircleAlert size={16} color={colors.textMuted} aria-hidden />
              <AppText variant="meta" muted style={{ flex: 1 }} testID="trade-problem">
                {p}
              </AppText>
            </View>
          ))
        )}
        <View style={{ flexDirection: 'row', gap: spacing.sm, flexWrap: 'wrap' }}>
          <Button
            label="Change team"
            variant="secondary"
            onPress={() => {
              setGet([]);
              setStep('team');
            }}
          />
          <Button
            label="Review offer"
            onPress={() => setStep('review')}
            disabled={!check.ok}
            testID="trade-next"
            style={{ flex: 1 }}
          />
        </View>
      </Card>
    ) : step === 'review' ? (
      <View style={{ flexDirection: 'row', gap: spacing.sm, flexWrap: 'wrap' }}>
        <Button label="Back" variant="secondary" onPress={() => setStep('players')} />
        <Button
          label="Send offer"
          onPress={send}
          loading={propose.isPending}
          disabled={!check.ok}
          testID="trade-send"
          style={{ flex: 1 }}
        />
      </View>
    ) : null;

  const notice = refused ? (
    <Card testID="trade-draft-notice" style={{ gap: spacing.xs, borderColor: colors.loss }}>
      <AppText weight="semibold">{`Your offer was refused: ${refused.refusal}`}</AppText>
      {refused.removed.length ? (
        <>
          <AppText variant="meta" muted>
            Taken out of the offer:
          </AppText>
          {refused.removed.map((r) => (
            <AppText
              key={r.player.id}
              variant="meta"
              testID={`trade-draft-removed-${r.player.id}`}
            >{`• ${r.player.displayName} ${r.reason}.`}</AppText>
          ))}
        </>
      ) : null}
    </Card>
  ) : null;

  return (
    <Screen
      title="Propose a trade"
      subtitle={
        step === 'team'
          ? 'Step 1 of 3 · pick a team'
          : step === 'players'
            ? 'Step 2 of 3 · pick players'
            : 'Step 3 of 3 · review and send'
      }
      back={{ label: 'Trades', href: withOrigin(`/leagues/${leagueId}/players/trades`, from) }}
    >
      {step !== 'team' ? notice : null}
      {step === 'players' && !isWide && !pastDeadline ? (
        <AppText variant="meta" muted testID="trade-summary">
          {`You give ${positionSummary(give)} · you get ${positionSummary(getList)}`}
        </AppText>
      ) : null}
      {step === 'players' && isWide ? footer : null}
      {body}
      {step === 'review' || (step === 'players' && !isWide) ? footer : null}
    </Screen>
  );
}
