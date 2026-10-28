import type { Envelope } from '@myfantasyprem/api-client';
import type { Href } from 'expo-router';
import type { ReactNode } from 'react';

import { AppText } from './AppText';
import { Card } from './Card';
import { QueryStatus, type QueryLike } from './QueryStatus';
import { Screen } from './Screen';
import { useFreshness } from '@/api/freshness';
import { formatAsOf } from '@/lib/time';

interface Props<T extends Envelope> {
  title: string;
  /** What the real screen will show. Placeholder copy only. */
  intent: string;
  back?: { label: string; href: Href };
  query: QueryLike<T>;
  /** Rendered once the query has data. */
  summary?: (data: T) => ReactNode;
  children?: ReactNode;
}

/** A route stub that proves its data hook is wired. Replace with real content. */
export function PlaceholderScreen<T extends Envelope>({
  title,
  intent,
  back,
  query,
  summary,
  children,
}: Props<T>) {
  const freshness = useFreshness(query);
  return (
    <Screen title={title} back={back} freshness={freshness}>
      <Card>
        <AppText variant="caption" muted>
          PLACEHOLDER
        </AppText>
        <AppText>{intent}</AppText>
      </Card>
      <QueryStatus query={query}>
        {(data) => (
          <Card testID="loaded">
            {summary ? summary(data) : null}
            <AppText variant="caption" muted>
              {`Data as of ${formatAsOf(Date.parse(data.meta.dataAsOf))}`}
            </AppText>
          </Card>
        )}
      </QueryStatus>
      {children}
    </Screen>
  );
}
