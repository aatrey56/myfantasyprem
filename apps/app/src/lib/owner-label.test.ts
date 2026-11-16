import { AVATAR_PALETTE, contrastRatio, userColor } from './identity';
import { firstNameOf, ownerChipColors, ownerChipLabel, parseOwnerLabel } from './owner-label';
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
    expect(firstNameOf('Ö')).toBe('Ö');
    expect(firstNameOf('Dr')).toBe('Dr');
  });

  it('is null for initials only, so the chip falls back to the team name', () => {
    expect(firstNameOf('A B')).toBeNull();
    expect(firstNameOf('A. B.')).toBeNull();
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

  it('says You on my own team in either mode', () => {
    expect(ownerChipLabel(team('Mine', 'Sam P.'), true, 'first_name').text).toBe('You');
    expect(ownerChipLabel(team('Mine', 'Sam P.'), true, 'team_name').text).toBe('You');
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

describe('parseOwnerLabel', () => {
  it('reads a stored choice, else the default', () => {
    expect(parseOwnerLabel('"team_name"')).toBe('team_name');
    expect(parseOwnerLabel('"first_name"')).toBe('first_name');
    expect(parseOwnerLabel(null)).toBe('first_name');
    expect(parseOwnerLabel('"initials"')).toBe('first_name');
    expect(parseOwnerLabel('{not json')).toBe('first_name');
  });
});
