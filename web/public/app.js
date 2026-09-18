const search = document.querySelector('[data-catalog-search]');
const cards = [...document.querySelectorAll('article[data-catalog-item]')];
const status = document.querySelector('[data-catalog-status]');
const categoryButtons = [...document.querySelectorAll('button[data-category-filter]')];
const clearSearch = document.querySelector('[data-catalog-clear-search]');
const emptyResults = document.querySelector('[data-empty-results]');
const emptyClearSearch = document.querySelector('[data-empty-clear-search]');
const emptyClearCategory = document.querySelector('[data-empty-clear-category]');
const themeToggle = document.querySelector('[data-theme-toggle]');

const updateThemeToggle = () => {
  if (!themeToggle) return;
  themeToggle.textContent = document.documentElement.dataset.theme === 'dark'
    ? 'Switch to light theme'
    : 'Switch to dark theme';
};

updateThemeToggle();

themeToggle?.addEventListener('click', () => {
  const nextTheme = document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark';
  document.documentElement.dataset.theme = nextTheme;
  try {
    window.localStorage.setItem('portal.theme', nextTheme);
  } catch {
    // The visual preference still applies for this page when storage is unavailable.
  }
  updateThemeToggle();
});

if (search && status) {
  let selectedCategory = '';

  const normalizedText = (element) =>
    (element?.textContent ?? '').trim().toLocaleLowerCase();

  const update = () => {
    const query = search.value.trim().toLocaleLowerCase();
    let visible = 0;

    for (const card of cards) {
      const name = normalizedText(card.querySelector('[data-catalog-name]'));
      const description = normalizedText(card.querySelector('[data-catalog-description]'));
      const category = normalizedText(card.querySelector('[data-catalog-category]'));
      const matchesSearch = [name, description, category].some((text) => text.includes(query));
      const matchesCategory = selectedCategory === '' || category === selectedCategory;
      const matches = matchesSearch && matchesCategory;
      card.hidden = !matches;
      visible += Number(matches);
    }

    status.textContent = `${visible} catalog ${visible === 1 ? 'item' : 'items'}`;
    if (clearSearch) {
      clearSearch.hidden = search.value.length === 0;
    }
    if (emptyResults) {
      emptyResults.hidden = visible !== 0 || (query === '' && selectedCategory === '');
    }
  };

  const selectCategory = (category) => {
    selectedCategory = category;
    for (const candidate of categoryButtons) {
      const candidateCategory = (candidate.dataset.categoryFilter ?? '').trim().toLocaleLowerCase();
      candidate.setAttribute('aria-pressed', String(candidateCategory === selectedCategory));
    }
    update();
  };

  search.addEventListener('input', update);

  clearSearch?.addEventListener('click', () => {
    search.value = '';
    update();
    search.focus();
  });

  emptyClearSearch?.addEventListener('click', () => {
    search.value = '';
    update();
    search.focus();
  });

  emptyClearCategory?.addEventListener('click', () => {
    selectCategory('');
  });

  for (const button of categoryButtons) {
    button.addEventListener('click', () => {
      selectCategory((button.dataset.categoryFilter ?? '').trim().toLocaleLowerCase());
    });
  }
}
