const search = document.querySelector('[data-catalog-search]');
const cards = [...document.querySelectorAll('article[data-catalog-item]')];
const status = document.querySelector('[data-catalog-status]');
const categoryButtons = [...document.querySelectorAll('button[data-category-filter]')];

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
  };

  search.addEventListener('input', update);

  for (const button of categoryButtons) {
    button.addEventListener('click', () => {
      selectedCategory = (button.dataset.categoryFilter ?? '').trim().toLocaleLowerCase();
      for (const candidate of categoryButtons) {
        candidate.setAttribute('aria-pressed', String(candidate === button));
      }
      update();
    });
  }
}
