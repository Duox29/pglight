import { describe, expect, it } from 'vitest'
import { formatSqlText } from './format'

describe('formatSqlText', () => {
  it('preserves the line ending after a line comment', () => {
    const sql = '-- schema setup\nCREATE TABLE users (notes text, -- manager column\nmanager_id bigint REFERENCES users(id) ON DELETE SET NULL);\n-- next statement\nCREATE INDEX users_notes ON users(notes);'

    const formatted = formatSqlText(sql)

    expect(formatted).toContain('-- schema setup\nCREATE TABLE')
    expect(formatted).toContain('-- manager column\nmanager_id')
    expect(formatted).toContain('ON DELETE\nSET NULL')
    expect(formatted).toContain('SET NULL);\n-- next statement\nCREATE INDEX')
  })

  it('does not treat comment-like text inside literals as comments', () => {
    const formatted = formatSqlText("SELECT '-- keep this', 'NULL'; -- real comment\nSELECT NULL;")

    expect(formatted).toContain("SELECT '-- keep this', 'NULL'")
    expect(formatted).toContain('-- real comment\nSELECT NULL')
  })
})
