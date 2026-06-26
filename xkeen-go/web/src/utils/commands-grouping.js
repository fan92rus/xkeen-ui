// utils/commands-grouping.js — pure logic that turns the flat command list
// returned by GET /api/xkeen/commands into the category-grouped shape the
// CommandsTab template renders. Extracted for unit testing.
//
// Backend item: { cmd, description, category, dangerous }
// Grouped item: { name: category, commands: [{ name, description, dangerous }] }
//
// IMPORTANT: the backend stores commands in a Go map and iterates it to build
// the response, so the input order is NON-deterministic (changes every
// process). To keep the Commands tab stable across page refreshes, this
// function applies a DETERMINISTIC sort:
//   - categories: by a fixed logical priority (matching xkeen -help order,
//     e.g. Установка … Удаление), unknown categories sorted alphabetically
//     after the known ones
//   - commands within a category: alphabetically by flag (cmd)

// Logical category order, matching how xkeen presents its own -help output.
// Categories not listed here fall after the known ones, sorted alphabetically.
const CATEGORY_PRIORITY = [
  'Установка',
  'Переустановка',
  'Обновление',
  'Запланированная задача автообновления GeoFile/GeoIPSET',
  'Резервная копия XKeen',
  'Резервная копия конфигурации Xray',
  'Резервная копия конфигурации Mihomo',
  'Удаление',
  'Порты проксирования',
  'Порты, исключённые из проксирования',
  'Управление прокси-клиентом',
  'Информация',
];

// Precompute a lookup: category → index in CATEGORY_PRIORITY.
const PRIORITY_INDEX = new Map(CATEGORY_PRIORITY.map((name, i) => [name, i]));

/**
 * Compare two category names for sorting. Known categories use their fixed
 * logical priority; unknown categories sort after them, alphabetically.
 */
function compareCategory(a, b) {
  const ia = PRIORITY_INDEX.get(a);
  const ib = PRIORITY_INDEX.get(b);
  if (ia !== undefined && ib !== undefined) return ia - ib;
  if (ia !== undefined) return -1; // a known, b unknown → a first
  if (ib !== undefined) return 1;  // a unknown, b known → b first
  // Both unknown — alphabetical, case-insensitive.
  return a.localeCompare(b, 'ru');
}

/**
 * Group a flat list of commands by their `category`, returning a DETERMINISTIC
 * order: categories sorted by logical priority (see CATEGORY_PRIORITY, unknown
 * alphabetically last) and commands sorted alphabetically by flag within each
 * category. The input order does not affect the output.
 *
 * @param {Array<{cmd:string,description:string,category:string,dangerous:boolean}>} commands
 * @returns {Array<{name:string,commands:Array<{name:string,description:string,dangerous:boolean}>}>}
 */
export function groupCommandsByCategory(commands) {
  const byCat = new Map();
  for (const c of commands || []) {
    const cat = c.category || 'Прочее';
    if (!byCat.has(cat)) byCat.set(cat, []);
    byCat.get(cat).push({
      name: c.cmd,
      description: c.description,
      dangerous: !!c.dangerous,
    });
  }

  const categories = Array.from(byCat.keys()).sort(compareCategory);
  return categories.map(name => ({
    name,
    commands: byCat.get(name).sort((a, b) => a.name.localeCompare(b.name)),
  }));
}
