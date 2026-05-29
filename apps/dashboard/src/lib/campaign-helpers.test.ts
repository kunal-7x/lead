import {
  splitLines,
  joinLines,
  parseObjections,
  joinObjections,
} from './campaign-helpers';

describe('splitLines', () => {
  it('splits on newline', () => {
    expect(splitLines('a\nb\nc')).toEqual(['a', 'b', 'c']);
  });
  it('splits on comma', () => {
    expect(splitLines('a,b,c')).toEqual(['a', 'b', 'c']);
  });
  it('trims and filters blanks', () => {
    expect(splitLines('  a  \n\n  b  ')).toEqual(['a', 'b']);
  });
  it('returns [] for empty string', () => {
    expect(splitLines('')).toEqual([]);
  });
});

describe('joinLines', () => {
  it('joins array with newlines', () => {
    expect(joinLines(['a', 'b', 'c'])).toBe('a\nb\nc');
  });
  it('returns empty string for empty array', () => {
    expect(joinLines([])).toBe('');
  });
});

describe('parseObjections', () => {
  it('parses valid lines', () => {
    const input = 'Too expensive :: We offer flexible plans\nNot interested :: Can I ask why?';
    expect(parseObjections(input)).toEqual([
      { objection: 'Too expensive', response: 'We offer flexible plans' },
      { objection: 'Not interested', response: 'Can I ask why?' },
    ]);
  });
  it('handles missing response', () => {
    const result = parseObjections('objection only');
    expect(result[0]!.objection).toBe('objection only');
    expect(result[0]!.response).toBe('');
  });
  it('handles double :: in response', () => {
    const result = parseObjections('Q :: A :: extra');
    expect(result[0]!.response).toBe('A :: extra');
  });
  it('filters blank lines', () => {
    expect(parseObjections('\n\n')).toEqual([]);
  });
});

describe('joinObjections', () => {
  it('formats array to text', () => {
    const arr = [
      { objection: 'Too expensive', response: 'Flexible plans' },
      { objection: 'Not interested', response: 'Tell me more' },
    ];
    expect(joinObjections(arr)).toBe(
      'Too expensive :: Flexible plans\nNot interested :: Tell me more',
    );
  });
  it('returns empty string for empty array', () => {
    expect(joinObjections([])).toBe('');
  });
});
