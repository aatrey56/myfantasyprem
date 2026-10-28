import { asSentence, joinSentences, withoutFullStop } from './errors';

describe('error sentences', () => {
  it('ends a sentence in exactly one stop', () => {
    expect(asSentence('FPL is down')).toBe('FPL is down.');
    expect(asSentence('  FPL is down.  ')).toBe('FPL is down.');
    expect(asSentence('Still there?')).toBe('Still there?');
    expect(asSentence('Wait!')).toBe('Wait!');
    expect(asSentence('Loading…')).toBe('Loading…');
    expect(asSentence('It said "no."')).toBe('It said "no."');
    expect(asSentence('(see above.)')).toBe('(see above.)');
    expect(asSentence('')).toBe('');
  });

  it('drops only a closing full stop', () => {
    expect(withoutFullStop('Player data is still loading. ')).toBe('Player data is still loading');
    expect(withoutFullStop('Really?')).toBe('Really?');
    expect(withoutFullStop('And so...')).toBe('And so...');
  });

  it('never doubles a full stop after the server detail', () => {
    expect(joinSentences('Import failed: Player data is still loading.', 'Nothing changed.')).toBe(
      'Import failed: Player data is still loading. Nothing changed.',
    );
    expect(joinSentences('Import failed: FPL is down', 'Try again.')).toBe(
      'Import failed: FPL is down. Try again.',
    );
    expect(joinSentences('Import failed: Is FPL down?', 'Try again')).toBe(
      'Import failed: Is FPL down? Try again.',
    );
  });

  it('does not repeat what the server already said', () => {
    expect(
      joinSentences(
        'Import failed: Player data is still loading. Try again in a few minutes.',
        'Try again.',
      ),
    ).toBe('Import failed: Player data is still loading. Try again in a few minutes.');
    expect(
      joinSentences('Import failed: The import could not finish. Try again.', 'Try again.'),
    ).toBe('Import failed: The import could not finish. Try again.');
    expect(joinSentences('Saved.', 'saved')).toBe('Saved.');
  });

  it('skips empty parts', () => {
    expect(joinSentences('Import failed: x', null, undefined, '  ', 'Try again.')).toBe(
      'Import failed: x. Try again.',
    );
    expect(joinSentences()).toBe('');
  });
});
