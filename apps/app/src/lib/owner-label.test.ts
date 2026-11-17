import { AVATAR_PALETTE, contrastRatio, userColor } from './identity';
import {
  DEFAULT_OWNER_LABEL,
  firstNameOf,
  managerNameOf,
  ownerChipColors,
  ownerChipLabel,
  parseOwnerLabel,
} from './owner-label';
import { darkColors } from '@/theme';

const team = (name: string, managerName: string) => ({ name, managerName });

describe('firstNameOf', () => {
  it('takes the first name of a display name', () => {
    expect(firstNameOf('Sam P.')).toBe('Sam');
    expect(firstNameOf('  Sam Rivers ')).toBe('Sam');
    expect(firstNameOf('Zoë')).toBe('Zoë');
    expect(firstNameOf('Jean-Luc Picard')).toBe('Jean-Luc');
  });

  it('keeps a leading lone initial with the next word', () => {
    expect(firstNameOf('J Doe')).toBe('J Doe');
    expect(firstNameOf('A. Smith')).toBe('A. Smith');
    expect(firstNameOf('j doe')).toBe('j doe');
  });

  it('skips leading titles, with or without a dot', () => {
    expect(firstNameOf('Dr Sam Rivers')).toBe('Sam');
    expect(firstNameOf('Mrs. J Doe')).toBe('J Doe');
    for (const t of ['Mr', 'Mr.', 'Ms', 'Miss', 'Sir', 'Prof', 'Prof.', 'dr.']) {
      expect(firstNameOf(`${t} Sam Rivers`)).toBe('Sam');
    }
  });

  it('shows a single word as it is', () => {
    expect(firstNameOf('Sam')).toBe('Sam');
    expect(firstNameOf('Öz')).toBe('Öz');
    expect(firstNameOf('Dr')).toBe('Dr');
  });

  it('is null for initials only, so the chip falls back to the team name', () => {
    expect(firstNameOf('A B')).toBeNull();
    expect(firstNameOf('A. B.')).toBeNull();
    // A single initial, alone or after an emoji, names no one either.
    expect(firstNameOf('A.')).toBeNull();
    expect(firstNameOf('J')).toBeNull();
    expect(firstNameOf('Ö')).toBeNull();
    expect(firstNameOf('⚽ A.')).toBeNull();
    expect(ownerChipLabel(team('Park The Bus FC', 'A.'), false, 'first_name').text).toBe(
      'Park The Bus FC',
    );
    expect(ownerChipLabel(team('Park The Bus FC', 'A B'), false, 'first_name').text).toBe(
      'Park The Bus FC',
    );
  });

  it('skips a leading emoji', () => {
    expect(firstNameOf('⚽ Sam')).toBe('Sam');
    expect(firstNameOf('⚽Sam Rivers')).toBe('Sam');
    expect(firstNameOf('🦁 J Doe')).toBe('J Doe');
  });

  it('keeps hyphenated and possessive names whole', () => {
    expect(firstNameOf('Mary-Jane Watson')).toBe('Mary-Jane');
    expect(firstNameOf("O'Neil Smith")).toBe("O'Neil");
    expect(firstNameOf('Sam’s Team')).toBe('Sam’s');
  });

  it('is null without a person: no letters, blank, an open seat', () => {
    expect(firstNameOf('🦁🔥')).toBeNull();
    expect(firstNameOf('   ')).toBeNull();
    expect(firstNameOf('Open seat')).toBeNull();
  });
});

describe('parseOwnerLabel', () => {
  it('reads each stored mode', () => {
    expect(parseOwnerLabel('"team_name"')).toBe('team_name');
    expect(parseOwnerLabel('"manager_name"')).toBe('manager_name');
    expect(parseOwnerLabel('"first_name"')).toBe('first_name');
  });

  it('defaults to team names for a device that never chose', () => {
    expect(DEFAULT_OWNER_LABEL).toBe('team_name');
    expect(parseOwnerLabel(null)).toBe('team_name');
  });

  it('falls back to the default on an unknown, malformed or wrong-typed value', () => {
    for (const raw of [
      '"initials"',
      '"TEAM_NAME"',
      'first_name',
      '{',
      '',
      '42',
      'null',
      '{}',
      '[]',
    ]) {
      expect(parseOwnerLabel(raw)).toBe('team_name');
    }
  });
});

describe('managerNameOf', () => {
  it('is the display name as set, trimmed', () => {
    expect(managerNameOf('Sam P.')).toBe('Sam P.');
    expect(managerNameOf('  Sam Rivers ')).toBe('Sam Rivers');
    expect(managerNameOf('⚽ Sam')).toBe('⚽ Sam');
  });

  it('is null when it names no one', () => {
    for (const name of ['', '   ', 'Open seat', 'Former manager', 'unclaimed', '🦁🔥']) {
      expect(managerNameOf(name)).toBeNull();
    }
  });
});

describe('ownerChipLabel', () => {
  it("shows the manager's first name, tinted with the team's avatar colour", () => {
    const label = ownerChipLabel(team('Sunday League Heroes', 'Sam P.'), false, 'first_name');
    expect(label.text).toBe('Sam');
    expect(label.color).toMatch(/^#[0-9A-F]{6}$/i);
    // The same team always gets the same colour.
    expect(ownerChipLabel(team('Sunday League Heroes', 'Ann'), false, 'team_name').color).toBe(
      label.color,
    );
  });

  it('falls back to the team name when the manager has no name', () => {
    expect(ownerChipLabel(team('Park The Bus FC', 'Open seat'), false, 'first_name').text).toBe(
      'Park The Bus FC',
    );
    expect(ownerChipLabel(team('Park The Bus FC', ''), false, 'first_name').text).toBe(
      'Park The Bus FC',
    );
  });

  it('says You on my own team in every mode', () => {
    for (const mode of ['team_name', 'manager_name', 'first_name'] as const) {
      expect(ownerChipLabel(team('Mine', 'Sam P.'), true, mode).text).toBe('You');
    }
  });

  it("shows the manager's display name in manager-name mode, whole", () => {
    expect(ownerChipLabel(team('Sunday League Heroes', 'Sam P.'), false, 'manager_name').text).toBe(
      'Sam P.',
    );
    expect(
      ownerChipLabel(team('Sunday League Heroes', ' Dr Sam Rivers '), false, 'manager_name').text,
    ).toBe('Dr Sam Rivers');
  });

  it('falls back to the team name in manager-name mode when no person is named', () => {
    for (const manager of ['', '  ', 'Open seat', 'Former manager', '🦁']) {
      expect(ownerChipLabel(team('Park The Bus FC', manager), false, 'manager_name').text).toBe(
        'Park The Bus FC',
      );
    }
  });

  it('falls back to Team when the team has no name either', () => {
    expect(ownerChipLabel(team('  ', ''), false, 'manager_name').text).toBe('Team');
    expect(ownerChipLabel(team('', 'Sam'), false, 'team_name').text).toBe('Team');
  });

  it('shows an emoji team name as the emoji, never ?', () => {
    expect(ownerChipLabel(team('🦁🔥', 'Dan W.'), false, 'team_name').text).toBe('🦁🔥');
    expect(ownerChipLabel(team('🦁🔥', '🦁'), false, 'first_name').text).toBe('🦁🔥');
  });

  it('shows the team name in team-name mode', () => {
    expect(ownerChipLabel(team("Gaffer's Army", 'Priya N.'), false, 'team_name').text).toBe(
      "Gaffer's Army",
    );
  });
});

describe('owner chip colour', () => {
  it("uses the team's avatar colour: the stored one, else the hashed one", () => {
    expect(
      ownerChipLabel({ ...team('Team A', 'Sam'), avatarColor: '#22C55E' }, false, 'first_name')
        .color,
    ).toBe('#22C55E');
    expect(ownerChipLabel(team('Team A', 'Sam'), false, 'first_name').color).toBe(
      userColor('Team A').bg,
    );
    expect(
      ownerChipLabel({ ...team('Team A', 'Sam'), avatarColor: 'red' }, false, 'first_name').color,
    ).toBe(userColor('Team A').bg);
  });

  // The app has one (dark) theme; every palette colour, yellow included,
  // keeps the chip text at WCAG AA.
  it.each([...AVATAR_PALETTE])('keeps chip text readable on %s (4.5:1)', (color) => {
    const { background, text } = ownerChipColors(color, false, darkColors);
    expect(contrastRatio(text, background)).toBeGreaterThanOrEqual(4.5);
  });

  it('keeps You readable on the accent', () => {
    const { background, text } = ownerChipColors('#EAB308', true, darkColors);
    expect(contrastRatio(text, background)).toBeGreaterThanOrEqual(4.5);
  });
});
