import { firstNameOf, ownerChipLabel, parseOwnerLabel } from './owner-label';

const team = (name: string, managerName: string) => ({ name, managerName });

describe('firstNameOf', () => {
  it('takes the first name of a display name', () => {
    expect(firstNameOf('Sam P.')).toBe('Sam');
    expect(firstNameOf('  Aatrey Sahay ')).toBe('Aatrey');
    expect(firstNameOf('Zoë')).toBe('Zoë');
    expect(firstNameOf('Jean-Luc Picard')).toBe('Jean-Luc');
  });

  it('skips a lone initial for the next word', () => {
    expect(firstNameOf('A. Smith')).toBe('Smith');
    expect(firstNameOf('Ö')).toBe('Ö');
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

describe('parseOwnerLabel', () => {
  it('reads a stored choice, else the default', () => {
    expect(parseOwnerLabel('"team_name"')).toBe('team_name');
    expect(parseOwnerLabel('"first_name"')).toBe('first_name');
    expect(parseOwnerLabel(null)).toBe('first_name');
    expect(parseOwnerLabel('"initials"')).toBe('first_name');
    expect(parseOwnerLabel('{not json')).toBe('first_name');
  });
});
