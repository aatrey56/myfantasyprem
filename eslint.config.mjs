import expo from 'eslint-config-expo/flat.js';
import prettier from 'eslint-config-prettier/flat';

export default [
  ...expo,
  prettier,
  {
    // Resolve the `@/` alias the way tsc and Metro do.
    settings: {
      'import/resolver': {
        typescript: {
          noWarnOnMultipleProjects: true,
          project: ['apps/app/tsconfig.json', 'packages/api-client/tsconfig.json'],
        },
      },
    },
  },
  {
    ignores: [
      '**/node_modules/**',
      '.claude/**',
      '**/dist/**',
      '**/dist-mocks/**',
      '**/dist-server/**',
      '**/.expo/**',
      '**/.stryker-tmp/**',
      'apps/app/public/**',
      'packages/api-client/src/schema.d.ts',
      'server/**',
      'playwright-report/**',
      'test-results/**',
    ],
  },
  {
    // Jest globals.
    files: ['**/*.test.ts', '**/*.test.tsx'],
    languageOptions: { globals: { jest: 'readonly' } },
  },
];
