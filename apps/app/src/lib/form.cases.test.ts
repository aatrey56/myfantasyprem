/**
 * The form cases shared with the Go server (server/internal/statuspage,
 * statuspage_test.go runs the same file). A rule change here fails there
 * until both sides agree, so the mocks and the server cannot drift.
 */
import cases from '../../../../server/internal/statuspage/testdata/form_cases.json';
import { form, formGameweek } from './form';

describe('shared form cases: formGameweek', () => {
  it.each(cases.gameweek.map((c) => [c.name, c] as const))('%s', (_, c) => {
    expect(formGameweek(c.current, c.currentFinished)).toBe(c.want);
  });
});

describe('shared form cases: form', () => {
  it.each(cases.form.map((c) => [c.name, c] as const))('%s', (_, c) => {
    expect(form(c.matches, c.formGameweek)).toBe(c.want);
  });
});
