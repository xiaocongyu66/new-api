# Nailao Features & Theme E2E Tests

This directory contains end-to-end tests for the ported Nailao features using Playwright.

## Test Scope

1. **Home Page**: Verifies hero, usage policy, and feature sections render in Chinese.
2. **User Insights**: Tests navigation, tab switching (Profiles / Evidence Samples / Profiling settings), summary statistics cards, and Chinese labels.
3. **Theme Settings**: Tests that the theme drawer lists the default cold-blue and the optional cheese-gold preset, and that selecting the gold preset keeps the page styled.
4. **User Insights Settings**: Verifies full i18n coverage for User Insight configuration fields on the profiling settings tab.
5. **Sidebar Navigation**: Ensures the Admin tab lists User Insights with a non-empty title and correct link.

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
