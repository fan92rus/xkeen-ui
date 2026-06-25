// utils/commands-grouping.js — pure logic that turns the flat command list
// returned by GET /api/xkeen/commands into the category-grouped shape the
// CommandsTab template renders. Extracted for unit testing.
//
// Backend item: { cmd, description, category, dangerous }
// Grouped item: { name: category, commands: [{ name, description, dangerous }] }

/**
 * Group a flat list of commands by their `category`, preserving first-seen
 * order of categories.
 *
 * @param {Array<{cmd:string,description:string,category:string,dangerous:boolean}>} commands
 * @returns {Array<{name:string,commands:Array<{name:string,description:string,dangerous:boolean}>}>}
 */
export function groupCommandsByCategory(commands) {
  const order = [];
  const byCat = new Map();
  for (const c of commands || []) {
    const cat = c.category || 'Прочее';
    if (!byCat.has(cat)) {
      byCat.set(cat, []);
      order.push(cat);
    }
    byCat.get(cat).push({
      name: c.cmd,
      description: c.description,
      dangerous: !!c.dangerous,
    });
  }
  return order.map(name => ({ name, commands: byCat.get(name) }));
}
