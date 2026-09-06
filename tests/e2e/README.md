# Nailao Features & Theme E2E Tests

This directory contains end-to-end tests for the ported Nailao features using Playwright.

## Test Scope

1. **Home Page**: Verifies Nailao-specific hero, story, policy, and feature sections render in Chinese.
2. **User Insights**: Tests navigation, tab switching (Profiles / Evidence Samples), summary statistics cards, and Chinese labels.
3. **Theme Settings**: Tests switching between the default cold-blue and the optional cheese-gold preset without label collisions.
4. **Operations Settings**: Verifies full i18n coverage for User Insight configuration fields.
5. **Sidebar Navigation**: Ensures correct item ordering and non-empty title/icon for User Insights.

## Running Tests

Ensure both backend (`:3000`) and frontend dev server (`:5173`) are running:

```bash
# Run tests with Playwright
npx playwright test --config=tests/e2e/playwright.config.ts

# Run with UI mode
npx playwright test --config=tests/e2e/playwright.config.ts --ui

# Run specific test
npx playwright test --config=tests/e2e/playwright.config.ts -g "User Insights"
```
