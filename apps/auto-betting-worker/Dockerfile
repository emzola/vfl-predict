FROM mcr.microsoft.com/playwright:v1.55.1-noble

WORKDIR /app
COPY package.json /app/package.json
RUN npm install --omit=dev
COPY index.mjs /app/index.mjs

ENV NODE_ENV=production
CMD ["npm", "start"]
