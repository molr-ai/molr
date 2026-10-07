/** Original Molr vector artwork, created for this repository. No third-party assets. */
export const assistantAvatarPresets = ['robot', 'cat', 'fox', 'bear', 'rabbit', 'panda', 'owl', 'frog', 'penguin', 'whale', 'sprout', 'star'] as const;
export type AssistantAvatarPreset = typeof assistantAvatarPresets[number];
const colors = ['#c6dcf4', '#f8d8c3', '#f7d594', '#e5d6bd', '#eed8ea', '#d2e5dc', '#ddd5f0', '#d5e9b8', '#cee1eb', '#c5e5ec', '#e0eac6', '#f8e5a9'];
export function AssistantAvatar({ name, avatar }: { name: string; avatar?: string }) {
 const id = avatar?.startsWith('preset:') ? avatar.slice(7) as AssistantAvatarPreset : undefined;
 const index = id ? assistantAvatarPresets.indexOf(id) : -1;
 if (index < 0) return avatar && /^data:image\/(png|jpeg);base64,/.test(avatar) ? <img className="assistant-avatar-art" src={avatar} alt="" /> : <>{Array.from(name)[0]?.toUpperCase()}</>;
 const ink = '#25483d', cream = '#fff8ea';
 const eyes = <><circle cx="25" cy="34" r="2.3" fill={ink} /><circle cx="39" cy="34" r="2.3" fill={ink} /><path d="M28 42 Q32 46 36 42" fill="none" stroke={ink} strokeWidth="2.5" strokeLinecap="round" /></>;
 return <svg className="assistant-avatar-art" viewBox="0 0 64 64" aria-hidden="true"><rect width="64" height="64" fill={colors[index]} />
 <g transform="translate(32 32) scale(0.85) translate(-32 -32)">
 {id === 'robot' ? <><path d="M32 12v7" stroke={ink} strokeWidth="3" /><circle cx="32" cy="10" r="4" fill="#ed956c" /><rect x="12" y="21" width="40" height="31" rx="11" fill={ink} /><rect x="17" y="26" width="30" height="20" rx="7" fill={cream} /><path d="M22 33v5m20-5v5m-14 2h8" stroke={ink} strokeWidth="3" strokeLinecap="round" /></>
 : id === 'whale' ? <><path d="M12 34c0-18 32-19 33 0 8 2 9-7 9-7 5 18-12 25-26 22-11-2-16-7-16-15" fill="#408b9f" /><path d="M26 18q-8-12-11-4m11 4q1-13 8-10" fill="none" stroke="#408b9f" strokeWidth="3" strokeLinecap="round" /><circle cx="24" cy="32" r="2.5" fill={ink} /><path d="M17 40q9 8 20 2" fill="none" stroke={cream} strokeWidth="3" /></>
 : id === 'sprout' ? <><path d="M32 38V19" stroke={ink} strokeWidth="3" /><path d="M32 29C12 31 12 13 14 13c15-2 20 10 18 16m0-5C31 9 48 8 51 10c0 14-9 19-19 14" fill="#5b936c" /><path d="M16 35h32l-5 19H21z" fill="#b97855" /><path d="M14 34h36v6H14z" fill="#d59a75" /><circle cx="27" cy="45" r="2" fill={ink} /><circle cx="37" cy="45" r="2" fill={ink} /></>
 : id === 'star' ? <><path d="m32 9 7 15 17 3-12 12 2 17-14-8-14 8 2-17L8 27l17-3z" fill="#dc9f3b" stroke={cream} strokeWidth="2" />{eyes}</>
 : <>
 {['cat','fox','owl'].includes(id!) && <path d="M13 31V12l17 11h4l17-11v19" fill={id === 'fox' ? '#c97949' : ink} />}
 {['bear','panda'].includes(id!) && <><circle cx="16" cy="19" r="9" fill={ink} /><circle cx="48" cy="19" r="9" fill={ink} /></>}
 {id === 'rabbit' && <><ellipse cx="23" cy="19" rx="7" ry="15" fill={cream} /><ellipse cx="41" cy="19" rx="7" ry="15" fill={cream} /><path d="M23 10v13m18-13v13" stroke="#d99ca8" strokeWidth="4" strokeLinecap="round" /></>}
 {id === 'frog' && <><circle cx="19" cy="22" r="9" fill="#69965a" /><circle cx="45" cy="22" r="9" fill="#69965a" /></>}
 <ellipse cx="32" cy="36" rx="22" ry="20" fill={id === 'fox' ? '#c97949' : id === 'frog' ? '#69965a' : ['panda','rabbit'].includes(id!) ? cream : ink} />
 <path d={id === 'fox' || id === 'owl' ? 'M12 29q10-2 20 12 10-14 20-12-2 24-20 24S14 48 12 29' : 'M17 35c0-19 30-19 30 0v7c0 15-30 15-30 0z'} fill={id === 'frog' ? '#bdd48d' : cream} />
 {id === 'panda' && <><ellipse cx="24" cy="33" rx="6" ry="7" fill={ink} /><ellipse cx="40" cy="33" rx="6" ry="7" fill={ink} /><circle cx="25" cy="33" r="2" fill={cream} /><circle cx="39" cy="33" r="2" fill={cream} /></>}
 {id !== 'panda' && eyes}
 {id === 'panda' && <path d="m28 42 4 3 4-3" fill="none" stroke={ink} strokeWidth="2.5" strokeLinecap="round" />}
 {id === 'penguin' && <path d="m27 40 5 6 5-6" fill="#e6ae4e" />}
 </>}
 </g></svg>;
}
