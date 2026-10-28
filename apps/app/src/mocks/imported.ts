/**
 * FPL Draft leagues imported into the app (ADR 0012), on top of the
 * in-season mock league: playable like any league from the start, with the
 * commissioner's re-sync from FPL and check against FPL (both queued, then
 * done on the next read of the status), the latest check's report, and the
 * seats with their claim links.
 */
import type { Schemas } from '@myfantasyprem/api-client';

import { conflict, MockError, notFound } from './errors';
import {
  createInSeasonLeague,
  DEMO_USER_ID,
  DEMO_USER_NAME,
  type InSeasonConfig,
  type InSeasonLeague,
} from './league';
import { CURRENT_GW, HOUR, mockId, NOW } from './world';

type Invite = Schemas['Invite'];
type SyncItem = Schemas['SyncItem'];

/** What `mockAppURL()` gives in preseason.ts: the page's origin on web. */
const appURL = () =>
  typeof location !== 'undefined' && location.origin
    ? location.origin
    : 'https://myfantasyprem.app';

interface Seat {
  claimed: boolean;
  owner: Schemas['UserRef'] | null;
  /** The FPL manager of the seat (real name in production, invented here). */
  fplManagerName: string;
  invite: Invite | null;
}

export interface ImportedConfig extends Omit<InSeasonConfig, 'review' | 'draftJitter'> {
  fplLeagueId: number;
  /** Whether the demo user has the seat at `myTeamIndex` yet (false: a claim link waits). */
  joined: boolean;
  /** Seats someone else has claimed already, by index. */
  claimedBy?: Record<number, string>;
  /** Fixed claim codes by seat index (the rest are generated). */
  claimCodes?: Record<number, string>;
  /** The latest verification's failing items (none: clean). */
  failing?: (teams: Schemas['TeamRef'][]) => SyncItem[];
  /** The latest automatic (shadow) check's differences; omitted: none has run. */
  shadow?: (teams: Schemas['TeamRef'][]) => SyncItem[];
  /**
   * A trade FPL accepted and has not processed or vetoed yet, between the
   * seats `from` and `to` (indices), one player a side per position: it
   * shows in review with no actions, and a re-sync keeps it.
   */
  pendingTrade?: { fplTradeId: number; from: number; to: number; positions: Schemas['Position'][] };
}

const CODE_ALPHABET = '23456789ABCDEFGHJKMNPQRSTVWXYZ';

export function createImportedLeague(cfg: ImportedConfig) {
  /** FPL's state of the league: what every re-sync resets it to. */
  const fromFpl = () => createInSeasonLeague({ ...cfg, review: 'commissioner', draftJitter: 0.2 });
  let base = fromFpl();
  const order = cfg.teams.map((_, i) => base.teamById(mockId('tm', cfg.idOffset + i + 1))!);
  const teamAt = (i: number) => base.teamById(order[i]!.id)!;
  let joined = cfg.joined;
  let pendingJob: Schemas['SyncPending'] | null = null;
  let lastSyncedAt = NOW - 6 * 60_000;
  let lastAttemptAt = lastSyncedAt;
  let checkedAt = lastSyncedAt;
  let note: string | null = null;
  let inviteSeq = 0;
  const explanations = new Map<string, Schemas['SyncExplanation']>();
  const failingItems = cfg.failing?.(order) ?? [];
  const pendingTrade = cfg.pendingTrade ? buildPendingTrade(cfg.pendingTrade) : null;

  /** Players a side by position, from each team's current roster. */
  function buildPendingTrade(p: NonNullable<ImportedConfig['pendingTrade']>) {
    const from = order[p.from]!;
    const to = order[p.to]!;
    const squad = (t: Schemas['TeamRef']) => base.teamDetail(t.id)!.roster.map((r) => r.player);
    const take = (players: Schemas['PlayerRef'][]) => {
      const used = new Map<string, number>();
      return p.positions.map((pos) => {
        const n = used.get(pos) ?? 0;
        used.set(pos, n + 1);
        return players.filter((x) => x.position === pos)[n]!;
      });
    };
    const offered = take(squad(from));
    const requested = take(squad(to));
    const accepted = NOW - HOUR;
    const trade: Schemas['Trade'] = {
      id: mockId('trd', cfg.idOffset + 900),
      status: 'in_review',
      offeringTeam: from,
      receivingTeam: to,
      offered,
      requested,
      message: null,
      createdAt: new Date(accepted - 60_000).toISOString(),
      expiresAt: new Date(accepted).toISOString(),
      respondedAt: new Date(accepted).toISOString(),
      // FPL decides it: no review window here.
      review: {
        mode: 'commissioner',
        endsAt: null,
        vetoVotes: 0,
        allowVotes: 0,
        votesNeeded: null,
        myVote: null,
        decidedBy: null,
      },
      processedAt: null,
      effectiveGameweek: CURRENT_GW + 1,
      failureReason: null,
      viewerActions: [],
    };
    const names = (ps: Schemas['PlayerRef'][]) => ps.map((x) => x.displayName).join(', ');
    const line = `${from.name} give ${names(offered)}; ${to.name} give ${names(requested)}`;
    const item: SyncItem = {
      key: `pending_trade:fpl${p.fplTradeId}`,
      kind: 'pending_trade',
      gameweek: CURRENT_GW + 1,
      team: from,
      status: 'pending',
      ours: line,
      theirs: line,
      note: `Wait for FPL to process or veto the pending trade between ${from.name} and ${to.name}.`,
      explanation: null,
    };
    return { trade, item };
  }
  const checkedTotal = order.length * (CURRENT_GW - 1) * 4 + order.length + 2;

  const newInvite = (index: number, code?: string): Invite => {
    inviteSeq++;
    const c =
      code ??
      Array.from(
        { length: 10 },
        (_, k) => CODE_ALPHABET[(cfg.seed * 7 + index * 13 + inviteSeq * 17 + k * 11) % 30],
      ).join('');
    return {
      id: mockId('inv', cfg.idOffset + 500 + inviteSeq),
      code: c,
      url: `${appURL()}/join/${c}`,
      createdAt: new Date(NOW - 2 * 24 * HOUR + inviteSeq * 60_000).toISOString(),
      expiresAt: null,
      maxUses: 1,
      uses: 0,
      revoked: false,
      claimTeam: teamAt(index),
    };
  };

  const seats = new Map<string, Seat>(
    order.map((t, i) => {
      const mine = i === cfg.myTeamIndex;
      const otherOwner = cfg.claimedBy?.[i];
      const claimed = (mine && joined) || otherOwner !== undefined;
      const fplManagerName = cfg.teams[i]![1];
      return [
        t.id,
        {
          claimed,
          owner:
            mine && joined
              ? { id: DEMO_USER_ID, displayName: DEMO_USER_NAME }
              : otherOwner
                ? { id: mockId('usr', cfg.idOffset + 100 + i), displayName: otherOwner }
                : null,
          fplManagerName,
          invite: claimed ? null : newInvite(i, cfg.claimCodes?.[i]),
        },
      ];
    }),
  );

  const summary = (): Schemas['LeagueSummary'] => ({
    ...base.summary(),
    source: 'fpl_import',
    fplLeagueId: cfg.fplLeagueId,
    memberCount: [...seats.values()].filter((s) => s.claimed).length,
  });
  const viewer = (): Schemas['LeagueViewer'] =>
    joined ? base.viewer() : { role: 'member', team: null };

  function verification(): Schemas['SyncVerification'] {
    const failing = failingItems.map((it) => ({
      ...it,
      explanation: explanations.get(it.key) ?? null,
    }));
    const mismatch = failing.filter((f) => f.status === 'mismatch').length;
    const unverified = failing.filter((f) => f.status === 'unverified').length;
    const pending = pendingTrade ? [pendingTrade.item] : [];
    return {
      status: mismatch > 0 ? 'mismatch' : unverified > 0 ? 'incomplete' : 'clean',
      checkedAt: new Date(checkedAt).toISOString(),
      counts: {
        ok: checkedTotal - failing.length - pending.length,
        mismatch,
        unverified,
        pending: pending.length,
      },
      failing,
      unexplained: failing.filter((f) => !f.explanation).length,
      pending,
    };
  }

  function sync(): Schemas['LeagueSync'] {
    const commissioner = base.isCommissioner() && joined;
    return {
      fplLeagueId: cfg.fplLeagueId,
      lastSyncedAt: new Date(lastSyncedAt).toISOString(),
      lastAttemptAt: new Date(lastAttemptAt).toISOString(),
      pending: pendingJob,
      lastError: null,
      note,
      verification: commissioner ? verification() : null,
      shadow: commissioner ? shadow() : null,
    };
  }

  function shadow(): Schemas['ShadowCheck'] | null {
    if (!cfg.shadow) return null;
    const items = cfg.shadow(order);
    const mismatches = items.filter((it) => it.status === 'mismatch');
    const pending = items.filter((it) => it.status === 'pending');
    return {
      checkedAt: new Date(NOW - 20 * 60_000).toISOString(),
      reason: 'bonus',
      gameweek: 7,
      status: mismatches.length > 0 ? 'mismatch' : pending.length > 0 ? 'incomplete' : 'clean',
      counts: { ok: 412, mismatch: mismatches.length, unverified: 0, pending: pending.length },
      mismatches,
      pending,
    };
  }

  const requireCommissioner = () => {
    if (!joined || !base.isCommissioner())
      throw new MockError(
        403,
        'not-commissioner',
        'Forbidden',
        'Only the commissioner can do that.',
      );
  };

  const queue = (kind: Schemas['SyncJobKind']): Schemas['LeagueSync'] => {
    requireCommissioner();
    if (pendingJob)
      throw conflict(
        'sync-pending',
        'Already running',
        'A re-sync or check is already queued or running.',
      );
    pendingJob = { kind, requestedAt: new Date().toISOString() };
    return sync();
  };

  const notice = (trades: number, claims: number) => {
    const parts = [
      trades > 0 ? `${trades} open ${trades === 1 ? 'trade' : 'trades'}` : null,
      claims > 0 ? `${claims} waiver ${claims === 1 ? 'claim' : 'claims'}` : null,
    ].filter(Boolean);
    return parts.length === 0
      ? 'Re-synced from FPL.'
      : `Re-synced from FPL. ${parts.join(' and ')} ${trades + claims === 1 ? 'was' : 'were'} cancelled.`;
  };

  /** Runs the queued job: a check refreshes the report, a re-sync resets the league to FPL's state. */
  function finishJob() {
    const job = pendingJob;
    if (!job) return;
    pendingJob = null;
    lastAttemptAt = Date.now();
    checkedAt = lastAttemptAt;
    if (job.kind !== 'resync') return;
    const fresh = fromFpl();
    // FPL's own pending trades and claims do not carry over either.
    fresh.cancelOpen();
    const closed = base.cancelOpen();
    fresh.adoptClosed(closed);
    base = fresh;
    lastSyncedAt = lastAttemptAt;
    note = notice(closed.trades.length, closed.claims.length);
  }

  /** The status as a read sees it: a queued job finishes first. */
  function status(): Schemas['LeagueSync'] {
    finishJob();
    return sync();
  }

  function resync(): Schemas['LeagueSync'] {
    return queue('resync');
  }

  function check(): Schemas['LeagueSync'] {
    return queue('check');
  }

  function explain(key: string, note: string): Schemas['LeagueSync'] {
    requireCommissioner();
    if (!failingItems.some((f) => f.key === key)) throw notFound('Failing item');
    explanations.set(key, {
      note,
      explainedBy: DEMO_USER_NAME,
      explainedAt: new Date().toISOString(),
    });
    return sync();
  }

  function seatList(): Schemas['Seat'][] {
    requireCommissioner();
    return order.map(({ id }) => {
      const s = seats.get(id)!;
      return {
        team: base.teamById(id)!,
        fplManagerName: s.fplManagerName,
        claimed: s.claimed,
        owner: s.owner,
        invite: s.invite,
      };
    });
  }

  function resendInvite(teamId: string): Invite {
    requireCommissioner();
    const s = seats.get(teamId);
    if (!s) throw notFound('Team');
    if (s.claimed) throw conflict('seat-claimed', 'Seat claimed', 'This team has a manager.');
    if (s.invite) s.invite.revoked = true;
    s.invite = newInvite(order.findIndex((t) => t.id === teamId));
    return s.invite;
  }

  /**
   * Sign-in links (docs/adr/0013): the latest code per seat, which
   * replaces older ones, and the used codes. A code starts with its team's
   * ULID so a fresh page (another browser) can still resolve it.
   */
  const signInLinks = new Map<string, string>();
  const usedSignInLinks = new Set<string>();
  let signInSeq = 0;

  function issueSignInLink(teamId: string): Schemas['SeatSignInLink'] {
    requireCommissioner();
    const s = seats.get(teamId);
    const team = base.teamById(teamId);
    if (!s || !team) throw notFound('Team');
    if (!s.owner)
      throw conflict(
        'seat-unclaimed',
        'Nobody has claimed this team',
        `Nobody has claimed ${team.name} yet. Send its claim link instead.`,
      );
    if (s.owner.id === DEMO_USER_ID)
      throw conflict(
        'has-sign-in',
        'Can sign in already',
        `${s.owner.displayName} can sign in with their own email, Apple or Google account.`,
      );
    signInSeq++;
    const code = `${teamId.slice(3)}-${String(cfg.seed * 1000 + signInSeq).padStart(16, 'x')}`;
    signInLinks.set(teamId, code);
    return {
      team,
      owner: s.owner,
      url: `${appURL()}/signin-link/${code}`,
      expiresAt: new Date(Date.now() + 7 * 24 * HOUR).toISOString(),
    };
  }

  /** The seat's owner a sign-in link signs in, once; undefined when not this league's or no longer valid. */
  function consumeSignInLink(code: string): Schemas['UserRef'] | undefined {
    const teamId = `tm_${code.slice(0, 26)}`;
    const s = seats.get(teamId);
    if (!s?.owner || s.owner.id === DEMO_USER_ID || usedSignInLinks.has(code)) return undefined;
    const latest = signInLinks.get(teamId);
    if (latest !== undefined && latest !== code) return undefined;
    usedSignInLinks.add(code);
    signInLinks.delete(teamId);
    return s.owner;
  }

  const seatByCode = (code: string) =>
    [...seats.entries()].find(([, s]) => s.invite?.code === code.toUpperCase());

  function invitePreview(code: string): Schemas['InvitePreview'] | undefined {
    const hit = seatByCode(code);
    if (!hit) return undefined;
    const [teamId, s] = hit;
    const commissioner = base.members().find((m) => m.role === 'commissioner')!;
    return {
      code: s.invite!.code,
      leagueId: cfg.id,
      leagueName: cfg.name,
      season: summary().season,
      status: 'in_season',
      teamCount: order.length,
      memberCount: summary().memberCount,
      commissionerName: commissioner.user.displayName,
      expiresAt: null,
      joinable: true,
      claimTeam: { teamName: base.teamById(teamId)!.name, fplManagerName: s.fplManagerName },
      claimSignIn: true,
    };
  }

  /** Claim codes already used, with their seat's team name (409 on reuse). */
  const usedCodes = new Map<string, string>();

  /** The demo user claims the seat behind `code` (only its own seat exists in the mocks). */
  function claim(code: string): Schemas['Membership'] {
    const used = usedCodes.get(code.toUpperCase());
    if (used)
      throw conflict(
        'seat-claimed',
        'Already claimed',
        `${used} has already been claimed with this link. If that wasn't you, ask the commissioner.`,
      );
    const hit = seatByCode(code);
    if (!hit) throw notFound('Invite');
    const [teamId, s] = hit;
    if (joined)
      throw conflict('already-member', 'Already a member', 'You are already in this league.', {
        leagueId: cfg.id,
      });
    // The mock viewer's team is fixed at `myTeamIndex`; a link for another seat
    // still claims that one, which keeps the demo to one team per league.
    const mine = teamAt(cfg.myTeamIndex);
    const seat = seats.get(mine.id)!;
    usedCodes.set(s.invite!.code, base.teamById(teamId)!.name);
    s.invite!.uses = 1;
    s.invite = null;
    seat.claimed = true;
    seat.invite = null;
    seat.owner = { id: DEMO_USER_ID, displayName: DEMO_USER_NAME };
    joined = true;
    return { league: summary(), team: mine, role: 'member' };
  }

  const own = {
    summary,
    viewer,
    home: () => ({ ...base.home(), league: summary(), viewer: viewer() }),
    // Not in it yet: undefined, which GET /v1/me/leagues skips (typed like the base's).
    myLeague: () =>
      (joined ? { ...base.myLeague(), league: summary() } : undefined) as ReturnType<
        typeof base.myLeague
      >,
    members: () => base.members().filter((m) => seats.get(m.team!.id)?.claimed),
    // The trade FPL accepted and has not processed or vetoed yet: listed
    // with the league's trades, decided by FPL (no actions here), and kept
    // as FPL has it by a re-sync.
    listTrades: (status?: string | null, teamId?: string | null) => {
      const all = base.listTrades(status, teamId);
      const pending = pendingTrade?.trade;
      const extra =
        pending &&
        (!status || status === 'open' || status === pending.status) &&
        (!teamId || pending.offeringTeam.id === teamId || pending.receivingTeam.id === teamId)
          ? [pending]
          : [];
      return [...extra, ...all];
    },
    getTrade: (tradeId: string) =>
      pendingTrade && tradeId === pendingTrade.trade.id
        ? pendingTrade.trade
        : base.getTrade(tradeId),
    reviewTrade: (tradeId: string, decision: 'approve' | 'veto') => {
      if (pendingTrade && tradeId === pendingTrade.trade.id)
        throw conflict(
          'pending-on-fpl',
          'Pending on FPL',
          'FPL processes or vetoes this trade; the next re-sync from FPL applies what it decided.',
        );
      return base.reviewTrade(tradeId, decision);
    },
    // imported
    imported: true as const,
    sync,
    status,
    resync,
    check,
    explain,
    seatList,
    resendInvite,
    issueSignInLink,
    consumeSignInLink,
    invitePreview,
    claim,
    claimedWith: (code: string) => usedCodes.has(code.toUpperCase()),
  };
  // Every read and move goes to the current league, which a re-sync replaces.
  const league = {} as Omit<InSeasonLeague, keyof typeof own> & typeof own;
  for (const key of Object.keys(base) as (keyof InSeasonLeague)[])
    Object.defineProperty(league, key, {
      get: () => base[key],
      enumerable: true,
      configurable: true,
    });
  return Object.defineProperties(league, Object.getOwnPropertyDescriptors(own));
}

export type ImportedLeague = ReturnType<typeof createImportedLeague>;
